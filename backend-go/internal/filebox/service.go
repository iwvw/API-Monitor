package filebox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultMaxFileSize     = 100 * 1024 * 1024
	defaultExpiryHours     = 24
	defaultCodeLength      = 8
	codeAlphabet           = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	maxAccessLogLimit      = 500
	multipartMemoryBudget  = 32 << 20
	voidRoomModeTemporary  = "temporary"
	voidRoomModePersistent = "persistent"
)

const textFormatMarkdown = "markdown"

// downloadDedupWindow 是「真实逻辑下载」去重窗口：同一 IP 在同一 code 上
// 于该窗口内的并发/重复 HTTP 请求（多线程分块、刷新重试等）只计一次下载。
const downloadDedupWindow = 60 * time.Second

type Authenticator interface {
	IsAuthenticated(context.Context, *http.Request) (bool, error)
}

type Service struct {
	cfg          config.Config
	store        *database.Store
	schema       database.SchemaEnsurer
	auth         Authenticator
	dataDir      string
	uploadsDir   string
	metadataFile string
	nodeProvider NodeStorageProvider
	voidRooms    map[string]*voidRoom
	voidMu       sync.Mutex
	// downloadDedup 记录每个「ip|code」最近一次计数的时刻，用于同一逻辑下载的并发/重复请求去重。
	downloadDedup map[string]int64
	dedupMu       sync.Mutex
}

type voidRoom struct {
	ID            string                      `json:"id"`
	OwnerToken    string                      `json:"-"`
	Mode          string                      `json:"mode"`
	CreatedAt     int64                       `json:"createdAt"`
	ExpiresAt     int64                       `json:"expiresAt"`
	Participants  map[string]*voidParticipant `json:"-"`
	Signals       []voidSignal                `json:"-"`
	NextSignalID  int64                       `json:"-"`
	Closed        bool                        `json:"closed"`
	LastHeartbeat int64                       `json:"lastHeartbeat"`
}

type voidParticipant struct {
	ID        string `json:"id"`
	Token     string `json:"-"`
	ClientID  string `json:"clientId,omitempty"`
	Role      string `json:"role"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
	LastSeen  int64  `json:"lastSeen"`
}

type publicVoidParticipant struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
	LastSeen  int64  `json:"lastSeen"`
	Online    bool   `json:"online"`
}

type voidSignal struct {
	ID        int64           `json:"id"`
	From      string          `json:"from"`
	To        string          `json:"to,omitempty"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt int64           `json:"createdAt"`
}

type voidSignalRequest struct {
	ParticipantID    string          `json:"participantId"`
	ParticipantToken string          `json:"participantToken"`
	To               string          `json:"to"`
	Type             string          `json:"type"`
	Payload          json.RawMessage `json:"payload"`
}

type voidParticipantRequest struct {
	Name     string `json:"name"`
	ClientID string `json:"clientId"`
}

type voidRoomRequest struct {
	Mode       string `json:"mode"`
	Persistent bool   `json:"persistent"`
}

type voidNetworkCandidate struct {
	Label  string `json:"label"`
	Origin string `json:"origin"`
}

type Settings struct {
	MaxFileSize         int64    `json:"max_file_size"`
	AllowedMIMETypes    []string `json:"allowed_mime_types"`
	DefaultExpiryHours  int      `json:"default_expiry_hours"`
	PublicUploadEnabled bool     `json:"public_upload_enabled"`
	UpdatedAt           *string  `json:"updated_at"`
}

type Entry struct {
	Code               string                 `json:"code"`
	Type               string                 `json:"type"`
	Content            *string                `json:"content,omitempty"`
	OriginalName       *string                `json:"originalName,omitempty"`
	Filename           string                 `json:"filename"`
	Path               *string                `json:"path,omitempty"`
	MIMEType           *string                `json:"mimetype,omitempty"`
	Size               int64                  `json:"size"`
	CreatedAt          int64                  `json:"createdAt"`
	Expiry             int64                  `json:"expiry"`
	BurnAfterReading   bool                   `json:"burnAfterReading"`
	Downloads          int64                  `json:"downloads"`
	MaxDownloads       int64                  `json:"maxDownloads"`
	AccessPasswordHash *string                `json:"accessPasswordHash,omitempty"`
	RequiresPassword   bool                   `json:"requiresPassword"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
	StorageType        string                 `json:"storageType"`
	ServerID           *string                `json:"serverId,omitempty"`
	RemotePath         *string                `json:"remotePath,omitempty"`
}

type PublicEntry struct {
	Code             string  `json:"code"`
	Type             string  `json:"type"`
	OriginalName     *string `json:"originalName,omitempty"`
	Filename         string  `json:"filename"`
	MIMEType         *string `json:"mimetype,omitempty"`
	Size             int64   `json:"size"`
	CreatedAt        int64   `json:"createdAt"`
	Expiry           int64   `json:"expiry"`
	BurnAfterReading bool    `json:"burnAfterReading"`
	Downloads        int64   `json:"downloads"`
	MaxDownloads     int64   `json:"maxDownloads"`
	RequiresPassword bool    `json:"requiresPassword"`
	TextFormat       string  `json:"textFormat,omitempty"`
	Preview          string  `json:"preview,omitempty"`
	StorageType      string  `json:"storageType,omitempty"`
	ServerID         *string `json:"serverId,omitempty"`
}

type AccessLog struct {
	ID        int64   `json:"id"`
	Code      string  `json:"code"`
	Action    string  `json:"action"`
	IPAddress *string `json:"ipAddress"`
	UserAgent *string `json:"userAgent"`
	CreatedAt string  `json:"createdAt"`
}

type requestMeta struct {
	ip        string
	userAgent string
}

type sharePayload struct {
	Type             string `json:"type"`
	Text             string `json:"text"`
	Expiry           string `json:"expiry"`
	BurnAfterReading any    `json:"burn_after_reading"`
	MaxDownloads     string `json:"max_downloads"`
	AccessPassword   string `json:"access_password"`
	Password         string `json:"password"`
}

func New(cfg config.Config, authenticator Authenticator) *Service {
	dataDir := filepath.Join(cfg.DataDir, "filebox")
	service := &Service{
		cfg:          cfg,
		store:        database.New(cfg),
		auth:         authenticator,
		dataDir:      dataDir,
		uploadsDir:   filepath.Join(dataDir, "uploads"),
		metadataFile: filepath.Join(dataDir, "metadata.json"),
		voidRooms:    map[string]*voidRoom{},
		downloadDedup: map[string]int64{},
	}
	_ = service.ensureDirs()
	_ = service.migrateJSONMetadata(context.Background())
	return service
}

func (s *Service) SetNodeProvider(provider NodeStorageProvider) {
	s.nodeProvider = provider
}

func (s *Service) getBackend(storageType string) StorageBackend {
	if storageType == "remote" && s.nodeProvider != nil {
		return NewRemoteBackend(s.nodeProvider)
	}
	return NewLocalBackend(s.uploadsDir)
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/filebox")
	path = strings.Trim(path, "/")
	parts := []string{}
	if path != "" {
		parts = strings.Split(path, "/")
	}

	switch {
	case len(parts) == 2 && parts[0] == "void" && parts[1] == "rooms" && r.Method == http.MethodPost:
		if !s.requireAuth(w, r) {
			return
		}
		s.createVoidRoom(w, r)
	case len(parts) == 2 && parts[0] == "void" && parts[1] == "rooms" && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.listVoidRooms(w, r)
	case len(parts) == 2 && parts[0] == "void" && parts[1] == "network-candidates" && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.voidNetworkCandidates(w, r)
	case len(parts) == 3 && parts[0] == "void" && parts[1] == "rooms" && r.Method == http.MethodGet:
		s.getVoidRoom(w, r, parts[2])
	case len(parts) == 3 && parts[0] == "void" && parts[1] == "rooms" && r.Method == http.MethodDelete:
		s.closeVoidRoom(w, r, parts[2])
	case len(parts) == 4 && parts[0] == "void" && parts[1] == "rooms" && parts[3] == "participants" && r.Method == http.MethodPost:
		s.joinVoidRoom(w, r, parts[2])
	case len(parts) == 4 && parts[0] == "void" && parts[1] == "rooms" && parts[3] == "signals" && r.Method == http.MethodPost:
		s.postVoidSignal(w, r, parts[2])
	case len(parts) == 4 && parts[0] == "void" && parts[1] == "rooms" && parts[3] == "signals" && r.Method == http.MethodGet:
		s.getVoidSignals(w, r, parts[2])
	case len(parts) == 2 && parts[0] == "retrieve" && r.Method == http.MethodGet:
		s.sendEntryMetadata(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "public" && r.Method == http.MethodGet:
		s.sendEntryMetadata(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "download" && r.Method == http.MethodGet:
		s.downloadEntry(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "d" && r.Method == http.MethodGet:
		s.downloadEntry(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "public" && parts[2] == "download" && r.Method == http.MethodGet:
		s.downloadEntry(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "public" && parts[2] == "verify" && r.Method == http.MethodPost:
		s.verifyPublicShare(w, r, parts[1])
	case len(parts) == 1 && (parts[0] == "share" || parts[0] == "shares") && r.Method == http.MethodPost:
		if !s.requireAuth(w, r) {
			return
		}
		s.createShare(w, r)
	case len(parts) == 1 && parts[0] == "access-logs" && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.listAccessLogs(w, r)
	case len(parts) == 1 && parts[0] == "settings":
		if !s.requireAuth(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.getSettings(w, r)
		case http.MethodPut:
			s.updateSettings(w, r)
		default:
			response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case len(parts) == 2 && parts[0] == "jobs" && parts[1] == "cleanup" && r.Method == http.MethodPost:
		if !s.requireAuth(w, r) {
			return
		}
		s.cleanupJob(w, r)
	case len(parts) == 1 && (parts[0] == "history" || parts[0] == "shares") && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.listShares(w, r)
	case len(parts) == 2 && parts[0] == "shares" && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.sendEntryMetadata(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "storage-nodes" && r.Method == http.MethodGet:
		if !s.requireAuth(w, r) {
			return
		}
		s.listStorageNodes(w, r)
	case len(parts) == 2 && parts[0] == "shares" && parts[1] == "init-upload" && r.Method == http.MethodPost:
		if !s.requireAuth(w, r) {
			return
		}
		s.initRemoteUpload(w, r)
	case len(parts) == 2 && parts[0] == "shares" && parts[1] == "complete-upload" && r.Method == http.MethodPost:
		if !s.requireAuth(w, r) {
			return
		}
		s.completeRemoteUpload(w, r)
	case len(parts) == 3 && parts[0] == "shares" && parts[2] == "transfer" && r.Method == http.MethodPost:
		if !s.requireAuth(w, r) {
			return
		}
		s.transferStorage(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "shares" && r.Method == http.MethodDelete:
		if !s.requireAuth(w, r) {
			return
		}
		s.deleteShare(w, r, parts[1])
	case len(parts) == 1 && parts[0] != "" && r.Method == http.MethodDelete:
		if !s.requireAuth(w, r) {
			return
		}
		s.deleteShare(w, r, parts[0])
	default:
		response.Error(w, http.StatusNotFound, "filebox route not implemented")
	}
}

func (s *Service) sendEntryMetadata(w http.ResponseWriter, r *http.Request, code string) {
	entry, err := s.GetEntry(r.Context(), code, false)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "File not found or expired"})
		return
	}
	_ = s.LogAccess(r.Context(), entry.Code, "retrieve", metaFromRequest(r))
	response.OK(w, publicEntry(entry))
}

func (s *Service) createVoidRoom(w http.ResponseWriter, r *http.Request) {
	s.cleanupVoidRooms()
	var payload voidRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		response.Error(w, http.StatusBadRequest, "invalid void room payload")
		return
	}
	mode := normalizeVoidRoomMode(payload)
	id, err := randomCode(8)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	ownerToken, err := randomToken(32)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UnixMilli()
	room := &voidRoom{
		ID:         id,
		OwnerToken: ownerToken,
		Mode:       mode,
		CreatedAt:  now,
		ExpiresAt:  voidRoomExpiry(now, mode),
		Participants: map[string]*voidParticipant{
			"owner": {
				ID:        "owner",
				Token:     ownerToken,
				Role:      "owner",
				Name:      "房主",
				CreatedAt: now,
				LastSeen:  now,
			},
		},
		LastHeartbeat: now,
	}
	if mode == voidRoomModePersistent {
		if err := s.savePersistentVoidRoom(r.Context(), room); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.voidMu.Lock()
	s.voidRooms[id] = room
	s.voidMu.Unlock()
	response.OK(w, map[string]interface{}{
		"roomId":             id,
		"id":                 id,
		"ownerToken":         ownerToken,
		"ownerParticipantId": "owner",
		"mode":               room.Mode,
		"expiresAt":          room.ExpiresAt,
	})
}

func (s *Service) listVoidRooms(w http.ResponseWriter, r *http.Request) {
	s.cleanupVoidRooms()
	if err := s.loadPersistentVoidRooms(r.Context()); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UnixMilli()
	s.voidMu.Lock()
	rooms := make([]*voidRoom, 0, len(s.voidRooms))
	for _, room := range s.voidRooms {
		if room == nil || room.Closed || isVoidRoomExpired(room, now) {
			continue
		}
		rooms = append(rooms, room)
	}
	sort.SliceStable(rooms, func(i, j int) bool {
		if rooms[i].Mode != rooms[j].Mode {
			return rooms[i].Mode == voidRoomModePersistent
		}
		if rooms[i].CreatedAt != rooms[j].CreatedAt {
			return rooms[i].CreatedAt > rooms[j].CreatedAt
		}
		return rooms[i].ID < rooms[j].ID
	})
	result := make([]map[string]interface{}, 0, len(rooms))
	for _, room := range rooms {
		result = append(result, adminVoidRoom(room, now))
	}
	s.voidMu.Unlock()
	response.OK(w, result)
}

func (s *Service) getVoidRoom(w http.ResponseWriter, r *http.Request, id string) {
	s.cleanupVoidRooms()
	if err := s.ensurePersistentVoidRoomLoaded(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.voidMu.Lock()
	room := s.activeVoidRoomLocked(id)
	if room == nil {
		s.voidMu.Unlock()
		response.Error(w, http.StatusNotFound, "void room not found")
		return
	}
	snapshot := publicVoidRoom(room, time.Now().UnixMilli())
	s.voidMu.Unlock()
	response.OK(w, snapshot)
}

func (s *Service) joinVoidRoom(w http.ResponseWriter, r *http.Request, id string) {
	s.cleanupVoidRooms()
	if err := s.ensurePersistentVoidRoomLoaded(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var payload voidParticipantRequest
	_ = json.NewDecoder(r.Body).Decode(&payload)
	clientID := normalizeVoidClientID(payload.ClientID)
	participantID, err := randomCode(8)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	participantToken, err := randomToken(32)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UnixMilli()
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		name = "访客"
	}
	participant := &voidParticipant{
		ID:        participantID,
		Token:     participantToken,
		Role:      "guest",
		Name:      name,
		CreatedAt: now,
		LastSeen:  now,
	}

	s.voidMu.Lock()
	room := s.activeVoidRoomLocked(id)
	if room == nil {
		s.voidMu.Unlock()
		response.Error(w, http.StatusNotFound, "void room not found")
		return
	}
	if clientID != "" {
		for _, existing := range room.Participants {
			if existing == nil || existing.Role != "guest" || existing.ClientID != clientID {
				continue
			}
			existing.Name = name
			existing.LastSeen = now
			readyPayload, _ := json.Marshal(publicVoidParticipantFrom(existing, now))
			s.appendVoidSignalLocked(room, existing.ID, "owner", "participant.ready", readyPayload, now)
			snapshot := publicVoidRoom(room, now)
			s.voidMu.Unlock()
			response.OK(w, map[string]interface{}{
				"participantId":    existing.ID,
				"participantToken": existing.Token,
				"room":             snapshot,
			})
			return
		}
	}
	participant.ClientID = clientID
	room.Participants[participantID] = participant
	joinedPayload, _ := json.Marshal(publicVoidParticipantFrom(participant, now))
	s.appendVoidSignalLocked(room, participantID, "owner", "participant.joined", joinedPayload, now)
	snapshot := publicVoidRoom(room, now)
	s.voidMu.Unlock()

	response.OK(w, map[string]interface{}{
		"participantId":    participantID,
		"participantToken": participantToken,
		"room":             snapshot,
	})
}

func (s *Service) postVoidSignal(w http.ResponseWriter, r *http.Request, id string) {
	s.cleanupVoidRooms()
	if err := s.ensurePersistentVoidRoomLoaded(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var payload voidSignalRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid signal payload")
		return
	}
	payload.ParticipantID = normalizeVoidParticipantID(payload.ParticipantID)
	payload.To = normalizeVoidParticipantID(payload.To)
	payload.Type = strings.TrimSpace(payload.Type)
	if payload.ParticipantID == "" || payload.ParticipantToken == "" || payload.Type == "" {
		response.Error(w, http.StatusBadRequest, "participant and signal type required")
		return
	}
	if !allowedVoidSignalType(payload.Type) {
		response.Error(w, http.StatusBadRequest, "unsupported void signal type")
		return
	}

	now := time.Now().UnixMilli()
	s.voidMu.Lock()
	room := s.activeVoidRoomLocked(id)
	if room == nil {
		s.voidMu.Unlock()
		response.Error(w, http.StatusNotFound, "void room not found")
		return
	}
	participant := room.Participants[payload.ParticipantID]
	if !validVoidParticipantToken(participant, payload.ParticipantToken) {
		s.voidMu.Unlock()
		response.Error(w, http.StatusForbidden, "invalid void participant token")
		return
	}
	if payload.To != "" {
		if _, ok := room.Participants[payload.To]; !ok {
			s.voidMu.Unlock()
			response.Error(w, http.StatusNotFound, "void signal target not found")
			return
		}
	}
	participant.LastSeen = now
	room.LastHeartbeat = now
	signal := s.appendVoidSignalLocked(room, payload.ParticipantID, payload.To, payload.Type, payload.Payload, now)
	s.voidMu.Unlock()
	response.OK(w, signal)
}

func (s *Service) getVoidSignals(w http.ResponseWriter, r *http.Request, id string) {
	s.cleanupVoidRooms()
	if err := s.ensurePersistentVoidRoomLoaded(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	participantID := normalizeVoidParticipantID(r.URL.Query().Get("participantId"))
	participantToken := strings.TrimSpace(r.URL.Query().Get("participantToken"))
	if participantToken == "" {
		participantToken = strings.TrimSpace(r.Header.Get("X-Void-Participant-Token"))
	}
	since, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("since")), 10, 64)
	if participantID == "" || participantToken == "" {
		response.Error(w, http.StatusBadRequest, "participant token required")
		return
	}

	now := time.Now().UnixMilli()
	s.voidMu.Lock()
	room := s.activeVoidRoomLocked(id)
	if room == nil {
		s.voidMu.Unlock()
		response.Error(w, http.StatusNotFound, "void room not found")
		return
	}
	participant := room.Participants[participantID]
	if !validVoidParticipantToken(participant, participantToken) {
		s.voidMu.Unlock()
		response.Error(w, http.StatusForbidden, "invalid void participant token")
		return
	}
	participant.LastSeen = now
	room.LastHeartbeat = now
	signals := make([]voidSignal, 0)
	for _, signal := range room.Signals {
		if signal.ID <= since || signal.From == participantID {
			continue
		}
		if signal.To == "" || signal.To == participantID {
			signals = append(signals, signal)
		}
	}
	snapshot := publicVoidRoom(room, now)
	s.voidMu.Unlock()
	response.OK(w, map[string]interface{}{"signals": signals, "room": snapshot})
}

func (s *Service) closeVoidRoom(w http.ResponseWriter, r *http.Request, id string) {
	s.cleanupVoidRooms()
	if err := s.ensurePersistentVoidRoomLoaded(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	ownerToken := strings.TrimSpace(r.URL.Query().Get("ownerToken"))
	if ownerToken == "" {
		ownerToken = strings.TrimSpace(r.Header.Get("X-Void-Owner-Token"))
	}
	if ownerToken == "" {
		var payload struct {
			OwnerToken string `json:"ownerToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		ownerToken = strings.TrimSpace(payload.OwnerToken)
	}
	adminOK := false
	if ownerToken == "" && s.auth != nil {
		ok, err := s.auth.IsAuthenticated(r.Context(), r)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		adminOK = ok
	}
	s.voidMu.Lock()
	room := s.activeVoidRoomLocked(id)
	if room == nil {
		s.voidMu.Unlock()
		response.Error(w, http.StatusNotFound, "void room not found")
		return
	}
	if !adminOK && (ownerToken == "" || ownerToken != room.OwnerToken) {
		s.voidMu.Unlock()
		response.Error(w, http.StatusForbidden, "invalid void owner token")
		return
	}
	roomID := room.ID
	persistent := room.Mode == voidRoomModePersistent
	s.voidMu.Unlock()
	if persistent {
		if err := s.markPersistentVoidRoomDeleted(r.Context(), roomID); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.voidMu.Lock()
	delete(s.voidRooms, roomID)
	s.voidMu.Unlock()
	response.OK(w, map[string]string{"status": "closed"})
}

func (s *Service) voidNetworkCandidates(w http.ResponseWriter, r *http.Request) {
	currentOrigin := requestOrigin(r)
	publicOrigin := strings.TrimRight(s.loadPublicAPIURL(r.Context()), "/")
	port := originPort(r.Host, s.cfg.Port)
	scheme := requestScheme(r)
	seen := map[string]bool{}
	candidates := []voidNetworkCandidate{}
	add := func(label, origin string) {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin == "" || seen[origin] {
			return
		}
		seen[origin] = true
		candidates = append(candidates, voidNetworkCandidate{Label: label, Origin: origin})
	}
	add("当前访问地址", currentOrigin)
	add("公网入口", publicOrigin)
	for _, origin := range localNetworkOrigins(scheme, port) {
		add("局域网地址", origin)
	}
	warnings := []string{}
	if isLoopbackHost(r.Host) {
		warnings = append(warnings, "当前链接包含 localhost 或 127.0.0.1，手机通常无法直接打开，请改用局域网地址。")
	}
	if s.cfg.Host == "127.0.0.1" || strings.EqualFold(s.cfg.Host, "localhost") {
		warnings = append(warnings, "后端当前只监听本机地址，局域网设备可能无法连接。")
	}
	response.OK(w, map[string]interface{}{
		"currentOrigin": currentOrigin,
		"publicOrigin":  publicOrigin,
		"listenHost":    s.cfg.Host,
		"candidates":    candidates,
		"warnings":      warnings,
	})
}

func (s *Service) cleanupVoidRooms() {
	now := time.Now().UnixMilli()
	s.voidMu.Lock()
	defer s.voidMu.Unlock()
	for id, room := range s.voidRooms {
		if room.ExpiresAt > 0 && now > room.ExpiresAt {
			delete(s.voidRooms, id)
		}
	}
}

func (s *Service) activeVoidRoomLocked(id string) *voidRoom {
	room := s.voidRooms[strings.ToUpper(strings.TrimSpace(id))]
	if room == nil || room.Closed {
		return nil
	}
	if isVoidRoomExpired(room, time.Now().UnixMilli()) {
		delete(s.voidRooms, room.ID)
		return nil
	}
	if room.Participants == nil {
		room.Participants = map[string]*voidParticipant{}
	}
	return room
}

func normalizeVoidRoomMode(payload voidRoomRequest) string {
	if payload.Persistent || strings.EqualFold(strings.TrimSpace(payload.Mode), voidRoomModePersistent) {
		return voidRoomModePersistent
	}
	return voidRoomModeTemporary
}

func voidRoomExpiry(now int64, mode string) int64 {
	if mode == voidRoomModePersistent {
		return 0
	}
	return now + int64(30*time.Minute/time.Millisecond)
}

func isVoidRoomExpired(room *voidRoom, now int64) bool {
	return room != nil && room.ExpiresAt > 0 && now > room.ExpiresAt
}

func (s *Service) savePersistentVoidRoom(ctx context.Context, room *voidRoom) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO filebox_void_rooms (id, owner_token, mode, created_at, expires_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, NULL)
	`, room.ID, room.OwnerToken, room.Mode, room.CreatedAt, room.ExpiresAt)
	if err != nil {
		return fmt.Errorf("save persistent void room: %w", err)
	}
	return nil
}

func (s *Service) markPersistentVoidRoomDeleted(ctx context.Context, id string) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		UPDATE filebox_void_rooms
		SET deleted_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, time.Now().UnixMilli(), normalizeCode(id))
	if err != nil {
		return fmt.Errorf("delete persistent void room: %w", err)
	}
	return nil
}

func (s *Service) ensurePersistentVoidRoomLoaded(ctx context.Context, id string) error {
	id = normalizeCode(id)
	if id == "" {
		return nil
	}
	s.voidMu.Lock()
	_, exists := s.voidRooms[id]
	s.voidMu.Unlock()
	if exists {
		return nil
	}
	room, err := s.loadPersistentVoidRoom(ctx, id)
	if err != nil || room == nil {
		return err
	}
	s.voidMu.Lock()
	if _, exists := s.voidRooms[room.ID]; !exists {
		s.voidRooms[room.ID] = room
	}
	s.voidMu.Unlock()
	return nil
}

func (s *Service) loadPersistentVoidRooms(ctx context.Context) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT id, owner_token, mode, created_at, expires_at
		FROM filebox_void_rooms
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
	`)
	if err != nil {
		return fmt.Errorf("load persistent void rooms: %w", err)
	}
	defer rows.Close()
	rooms := []*voidRoom{}
	for rows.Next() {
		room, err := scanPersistentVoidRoom(rows)
		if err != nil {
			return err
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.voidMu.Lock()
	for _, room := range rooms {
		if _, exists := s.voidRooms[room.ID]; !exists {
			s.voidRooms[room.ID] = room
		}
	}
	s.voidMu.Unlock()
	return nil
}

func (s *Service) loadPersistentVoidRoom(ctx context.Context, id string) (*voidRoom, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	row := db.QueryRowContext(ctx, `
		SELECT id, owner_token, mode, created_at, expires_at
		FROM filebox_void_rooms
		WHERE id = ? AND deleted_at IS NULL
	`, normalizeCode(id))
	room, err := scanPersistentVoidRoom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return room, err
}

type persistentVoidRoomScanner interface {
	Scan(dest ...interface{}) error
}

func scanPersistentVoidRoom(scanner persistentVoidRoomScanner) (*voidRoom, error) {
	var room voidRoom
	if err := scanner.Scan(&room.ID, &room.OwnerToken, &room.Mode, &room.CreatedAt, &room.ExpiresAt); err != nil {
		return nil, err
	}
	room.ID = normalizeCode(room.ID)
	room.Mode = normalizeVoidRoomMode(voidRoomRequest{Mode: room.Mode})
	now := time.Now().UnixMilli()
	room.Participants = map[string]*voidParticipant{
		"owner": {
			ID:        "owner",
			Token:     room.OwnerToken,
			Role:      "owner",
			Name:      "房主",
			CreatedAt: room.CreatedAt,
			LastSeen:  0,
		},
	}
	room.LastHeartbeat = now
	return &room, nil
}

func (s *Service) appendVoidSignalLocked(room *voidRoom, from, to, signalType string, payload json.RawMessage, now int64) voidSignal {
	room.NextSignalID++
	signal := voidSignal{
		ID:        room.NextSignalID,
		From:      from,
		To:        to,
		Type:      signalType,
		Payload:   payload,
		CreatedAt: now,
	}
	room.Signals = append(room.Signals, signal)
	if len(room.Signals) > 1000 {
		room.Signals = append([]voidSignal(nil), room.Signals[len(room.Signals)-1000:]...)
	}
	return signal
}

func publicVoidRoom(room *voidRoom, now int64) map[string]interface{} {
	participants := make([]publicVoidParticipant, 0, len(room.Participants))
	for _, participant := range room.Participants {
		participants = append(participants, publicVoidParticipantFrom(participant, now))
	}
	sort.SliceStable(participants, func(i, j int) bool {
		if participants[i].Role != participants[j].Role {
			return participants[i].Role == "owner"
		}
		if participants[i].CreatedAt != participants[j].CreatedAt {
			return participants[i].CreatedAt < participants[j].CreatedAt
		}
		return participants[i].ID < participants[j].ID
	})
	mode := normalizeVoidRoomMode(voidRoomRequest{Mode: room.Mode})
	return map[string]interface{}{
		"id":           room.ID,
		"roomId":       room.ID,
		"mode":         mode,
		"persistent":   mode == voidRoomModePersistent,
		"createdAt":    room.CreatedAt,
		"expiresAt":    room.ExpiresAt,
		"closed":       room.Closed,
		"lastSignalId": room.NextSignalID,
		"participants": participants,
	}
}

func adminVoidRoom(room *voidRoom, now int64) map[string]interface{} {
	snapshot := publicVoidRoom(room, now)
	snapshot["ownerToken"] = room.OwnerToken
	snapshot["ownerParticipantId"] = "owner"
	return snapshot
}

func publicVoidParticipantFrom(participant *voidParticipant, now int64) publicVoidParticipant {
	return publicVoidParticipant{
		ID:        participant.ID,
		Role:      participant.Role,
		Name:      participant.Name,
		CreatedAt: participant.CreatedAt,
		LastSeen:  participant.LastSeen,
		Online:    now-participant.LastSeen <= int64(45*time.Second/time.Millisecond),
	}
}

func validVoidParticipantToken(participant *voidParticipant, token string) bool {
	return participant != nil && participant.Token != "" && token == participant.Token
}

func normalizeVoidParticipantID(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "owner") {
		return "owner"
	}
	return strings.ToUpper(value)
}

func normalizeVoidClientID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			builder.WriteRune(char)
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == '-' || char == '_':
			builder.WriteRune(char)
		}
		if builder.Len() >= 96 {
			break
		}
	}
	return builder.String()
}

func allowedVoidSignalType(signalType string) bool {
	switch signalType {
	case "participant.ready", "webrtc.offer", "webrtc.answer", "webrtc.ice":
		return true
	default:
		return false
	}
}

func randomToken(length int) (string, error) {
	if length < 16 {
		length = 16
	}
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate void token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func (s *Service) loadPublicAPIURL(ctx context.Context) string {
	db, err := s.store.Open(ctx)
	if err != nil {
		return ""
	}
	defer db.Close()
	var value sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT public_api_url FROM user_settings WHERE id = 1`).Scan(&value); err != nil {
		return ""
	}
	return strings.TrimSpace(value.String)
}

func requestOrigin(r *http.Request) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "localhost"
	}
	return requestScheme(r) + "://" + host
}

func requestScheme(r *http.Request) string {
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		if idx := strings.Index(proto, ","); idx >= 0 {
			proto = strings.TrimSpace(proto[:idx])
		}
		if proto == "http" || proto == "https" {
			return proto
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func originPort(host string, fallback int) int {
	if _, portText, err := net.SplitHostPort(host); err == nil {
		if port, parseErr := strconv.Atoi(portText); parseErr == nil {
			return port
		}
	}
	return fallback
}

func localNetworkOrigins(scheme string, port int) []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	origins := []string{}
	seen := map[string]bool{}
	for _, item := range interfaces {
		if item.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := item.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			default:
				continue
			}
			ip = ip.To4()
			if ip == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
				continue
			}
			host := ip.String()
			origin := scheme + "://" + host
			if port > 0 {
				origin = fmt.Sprintf("%s://%s:%d", scheme, host, port)
			}
			if !seen[origin] {
				seen[origin] = true
				origins = append(origins, origin)
			}
		}
	}
	return origins
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Service) downloadEntry(w http.ResponseWriter, r *http.Request, code string) {
	entry, err := s.GetEntry(r.Context(), code, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entry == nil {
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}
	if !verifyAccessPassword(entry, accessPasswordFromRequest(r)) {
		http.Error(w, "Password required or invalid", http.StatusForbidden)
		return
	}

	// 先原子占用下载名额（计数/烧毁删除）再发送内容：并发请求同时通过的
	// MaxDownloads 检查不会全部放行，超卖窗口关闭。
	if err := s.AccessEntry(context.Background(), entry.Code, metaFromRequest(r)); err != nil {
		http.Error(w, "Download quota exceeded or entry expired", http.StatusForbidden)
		return
	}

	if entry.Type == "text" {
		contentType := "text/plain; charset=utf-8"
		if entry.MIMEType != nil && *entry.MIMEType != "" {
			contentType = *entry.MIMEType
		} else if entryTextFormat(entry) == textFormatMarkdown {
			contentType = "text/markdown; charset=utf-8"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": entry.Filename}))
		if entry.Content != nil {
			_, _ = io.WriteString(w, *entry.Content)
		}
		return
	}

	if entry.StorageType == "remote" {
		if s.nodeProvider == nil || entry.ServerID == nil || *entry.ServerID == "" {
			http.Error(w, "Storage node unavailable", http.StatusServiceUnavailable)
			return
		}
		serverID := *entry.ServerID
		node, err := s.nodeProvider.GetEligibleStorageNode(r.Context(), serverID)
		if err != nil || node == nil {
			http.Error(w, fmt.Sprintf("Storage node offline: %v", err), http.StatusServiceUnavailable)
			return
		}
		key, err := s.nodeProvider.GetStorageNodeAgentKey(r.Context(), serverID)
		if err != nil {
			http.Error(w, "Failed to resolve node credential", http.StatusInternalServerError)
			return
		}
		signedURL, err := BuildSignedURL("GET", node.Host, node.StoragePort, entry.Code, entry.Filename, 0, 5*time.Minute, key)
		if err != nil {
			http.Error(w, "Failed to build signed download URL", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, signedURL, http.StatusFound)
		return
	}

	if entry.Path == nil || *entry.Path == "" {
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}
	file, err := os.Open(*entry.Path)
	if err != nil {
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}
	name := entry.Filename
	if entry.OriginalName != nil && *entry.OriginalName != "" {
		name = *entry.OriginalName
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	if entry.MIMEType != nil && *entry.MIMEType != "" {
		w.Header().Set("Content-Type", *entry.MIMEType)
	}
	http.ServeContent(w, r, name, stat.ModTime(), file)
	_ = file.Close()
}

func (s *Service) verifyPublicShare(w http.ResponseWriter, r *http.Request, code string) {
	entry, err := s.GetEntry(r.Context(), code, false)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		response.JSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "File not found or expired"})
		return
	}
	verified := verifyAccessPassword(entry, accessPasswordFromRequest(r))
	status := http.StatusOK
	if !verified {
		status = http.StatusForbidden
	}
	response.JSON(w, status, map[string]interface{}{"success": verified, "requiresPassword": entry.RequiresPassword})
}

func (s *Service) createShare(w http.ResponseWriter, r *http.Request) {
	settings, err := s.LoadSettings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	payload, fileHeader, err := parseShareRequest(w, r, settings.MaxFileSize)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	expiryHours := parseExpiryHours(payload.Expiry, settings.DefaultExpiryHours)
	burn := parseBool(payload.BurnAfterReading)
	maxDownloads := parseNonNegativeInt64(payload.MaxDownloads)
	accessPassword := payload.AccessPassword
	if accessPassword == "" {
		accessPassword = payload.Password
	}

	var entry *Entry
	if strings.EqualFold(payload.Type, "text") {
		if strings.TrimSpace(payload.Text) == "" {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "Text content missing"})
			return
		}
		entry, err = s.AddText(r.Context(), payload.Text, expiryHours, burn, maxDownloads, accessPassword)
	} else {
		if fileHeader == nil {
			response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "No file uploaded"})
			return
		}
		entry, err = s.AddFile(r.Context(), fileHeader, expiryHours, burn, maxDownloads, accessPassword, settings)
	}
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"code":    entry.Code,
		"data":    publicEntry(entry),
		"expiry":  entry.Expiry,
	})
}

func (s *Service) listShares(w http.ResponseWriter, r *http.Request) {
	entries, err := s.GetAll(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, entries)
}

func (s *Service) deleteShare(w http.ResponseWriter, r *http.Request, code string) {
	if _, err := s.DeleteEntry(r.Context(), code); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) listAccessLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := s.GetAccessLogs(r.Context(), r.URL.Query().Get("code"), r.URL.Query().Get("limit"))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, logs)
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.LoadSettings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) updateSettings(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	settings, err := s.UpdateSettings(r.Context(), payload)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) cleanupJob(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.CleanupExpired(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]int{"deleted": deleted})
}

func (s *Service) LoadSettings(ctx context.Context) (Settings, error) {
	db, err := s.open(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer db.Close()
	return loadSettings(ctx, db)
}

func (s *Service) UpdateSettings(ctx context.Context, input map[string]interface{}) (Settings, error) {
	current, err := s.LoadSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	next := Settings{
		MaxFileSize:         positiveInt64(input["max_file_size"], current.MaxFileSize),
		AllowedMIMETypes:    stringSlice(input["allowed_mime_types"], current.AllowedMIMETypes),
		DefaultExpiryHours:  int(positiveInt64(input["default_expiry_hours"], int64(current.DefaultExpiryHours))),
		PublicUploadEnabled: boolValue(input["public_upload_enabled"], current.PublicUploadEnabled),
	}
	if next.MaxFileSize < 1 {
		next.MaxFileSize = defaultMaxFileSize
	}
	if next.DefaultExpiryHours < 1 {
		next.DefaultExpiryHours = defaultExpiryHours
	}

	db, err := s.open(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer db.Close()
	encodedMimeTypes, _ := json.Marshal(next.AllowedMIMETypes)
	_, err = db.ExecContext(ctx, `
		UPDATE filebox_settings
		SET max_file_size = ?,
			allowed_mime_types = ?,
			default_expiry_hours = ?,
			public_upload_enabled = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, next.MaxFileSize, string(encodedMimeTypes), next.DefaultExpiryHours, boolInt(next.PublicUploadEnabled))
	if err != nil {
		return Settings{}, fmt.Errorf("update filebox settings: %w", err)
	}
	return loadSettings(ctx, db)
}

func (s *Service) AddText(ctx context.Context, content string, expiryHours float64, burnAfterReading bool, maxDownloads int64, accessPassword string) (*Entry, error) {
	code, err := s.GenerateCode(ctx, defaultCodeLength)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	expiry := expiryTime(now, expiryHours)
	passwordHash, err := hashAccessPassword(accessPassword)
	if err != nil {
		return nil, err
	}
	filename := "text_" + code + ".md"
	mimeType := "text/markdown; charset=utf-8"
	metadataJSON, err := json.Marshal(map[string]string{"textFormat": textFormatMarkdown})
	if err != nil {
		return nil, fmt.Errorf("encode filebox text metadata: %w", err)
	}

	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO filebox_entries (
			code, type, content, filename, mimetype, size, created_at, expiry,
			burn_after_reading, max_downloads, access_password_hash, metadata_json, storage_type
		) VALUES (?, 'text', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'local')
	`, code, content, filename, mimeType, len([]byte(content)), now, expiry, boolInt(burnAfterReading), maxDownloads, passwordHash, string(metadataJSON))
	if err != nil {
		return nil, fmt.Errorf("create filebox text share: %w", err)
	}
	return s.GetEntry(ctx, code, true)
}

func (s *Service) AddFile(ctx context.Context, fileHeader *multipart.FileHeader, expiryHours float64, burnAfterReading bool, maxDownloads int64, accessPassword string, settings Settings) (*Entry, error) {
	if err := s.ensureDirs(); err != nil {
		return nil, err
	}
	if fileHeader.Size > settings.MaxFileSize {
		return nil, fmt.Errorf("file too large, max %d bytes", settings.MaxFileSize)
	}
	mimeType := strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
	if !isMIMEAllowed(mimeType, settings.AllowedMIMETypes) {
		return nil, fmt.Errorf("file type not allowed: %s", emptyDefault(mimeType, "unknown"))
	}
	code, err := s.GenerateCode(ctx, defaultCodeLength)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	expiry := expiryTime(now, expiryHours)
	passwordHash, err := hashAccessPassword(accessPassword)
	if err != nil {
		return nil, err
	}
	safeName := sanitizeFilename(fileHeader.Filename)
	saveFilename := fmt.Sprintf("%d-%s-%s", now, code, safeName)
	savePath := filepath.Join(s.uploadsDir, saveFilename)
	if !isPathInside(s.uploadsDir, savePath) {
		return nil, errors.New("invalid upload path")
	}
	if err := saveUploadedFile(fileHeader, savePath); err != nil {
		return nil, err
	}

	db, err := s.open(ctx)
	if err != nil {
		_ = os.Remove(savePath)
		return nil, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO filebox_entries (
			code, type, original_name, filename, path, mimetype, size, created_at, expiry,
			burn_after_reading, max_downloads, access_password_hash, storage_type
		) VALUES (?, 'file', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'local')
	`, code, fileHeader.Filename, saveFilename, savePath, nullString(mimeType), fileHeader.Size, now, expiry, boolInt(burnAfterReading), maxDownloads, passwordHash)
	if err != nil {
		_ = os.Remove(savePath)
		return nil, fmt.Errorf("create filebox file share: %w", err)
	}
	return s.GetEntry(ctx, code, true)
}

func (s *Service) AddRemoteFile(ctx context.Context, code string, filename string, size int64, mimeType string, serverID string, expiryHours float64, burnAfterReading bool, maxDownloads int64, accessPassword string) (*Entry, error) {
	now := time.Now().UnixMilli()
	code = normalizeCode(code)
	expiry := expiryTime(now, expiryHours)
	passwordHash, err := hashAccessPassword(accessPassword)
	if err != nil {
		return nil, err
	}
	remotePath := fmt.Sprintf("shares/%s/%s", code, filename)

	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		INSERT INTO filebox_entries (
			code, type, original_name, filename, remote_path, mimetype, size, created_at, expiry,
			burn_after_reading, max_downloads, access_password_hash, storage_type, server_id
		) VALUES (?, 'file', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'remote', ?)
	`, code, filename, filename, remotePath, nullString(mimeType), size, now, expiry, boolInt(burnAfterReading), maxDownloads, passwordHash, serverID)
	if err != nil {
		return nil, fmt.Errorf("create remote filebox share: %w", err)
	}
	return s.GetEntry(ctx, code, true)
}

func (s *Service) GenerateCode(ctx context.Context, length int) (string, error) {
	if length < 1 {
		length = defaultCodeLength
	}
	for i := 0; i < 32; i++ {
		code, err := randomCode(length)
		if err != nil {
			return "", err
		}
		exists, err := s.Exists(ctx, code)
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", errors.New("failed to generate unique filebox code")
}

func (s *Service) Exists(ctx context.Context, code string) (bool, error) {
	db, err := s.open(ctx)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var found string
	err = db.QueryRowContext(ctx, `SELECT code FROM filebox_entries WHERE code = ? AND deleted_at IS NULL`, normalizeCode(code)).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check filebox code: %w", err)
	}
	return true, nil
}

func (s *Service) GetEntry(ctx context.Context, code string, includeExpired bool) (*Entry, error) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	entry, err := findEntry(ctx, db, normalizeCode(code))
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	if !includeExpired && isExpired(entry.Expiry) {
		_, _ = s.DeleteEntry(context.Background(), entry.Code)
		return nil, nil
	}
	if !includeExpired && entry.MaxDownloads > 0 && entry.Downloads >= entry.MaxDownloads {
		_, _ = s.DeleteEntry(context.Background(), entry.Code)
		return nil, nil
	}
	return entry, nil
}

func (s *Service) AccessEntry(ctx context.Context, code string, meta requestMeta) error {
	// 同一 IP 在同一去重窗口内对同一 code 的并发/重复请求视为同一次逻辑下载，
	// 在 GetEntry 之前放行，避免并发分块后续请求先触发 GetEntry 的 max_downloads 删除，
	// 也保证不重复计数、不重复扣配额、不重复触发阅后即焚、不重复写访问日志。
	if !s.claimDownloadSlot(meta.ip, code) {
		return nil
	}
	entry, err := s.GetEntry(ctx, code, false)
	if err != nil || entry == nil {
		return err
	}
	// 下载计数用条件原子更新：并发下载时由数据库判定是否放行，
	// 避免读-改-写竞态导致超过 max_downloads 的超额访问。
	db, openErr := s.open(ctx)
	if openErr != nil {
		return openErr
	}
	result, err := db.ExecContext(
		ctx,
		`UPDATE filebox_entries SET downloads = downloads + 1 WHERE code = ? AND (max_downloads = 0 OR downloads < max_downloads)`,
		entry.Code,
	)
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// 已达下载上限或条目已被并发删除：丢弃本次下载请求（返回错误让上游 403），
		// 并清理残留条目，避免 max_downloads 超卖依旧放行。
		_ = s.LogAccess(ctx, entry.Code, "download", meta)
		_, _ = s.DeleteEntry(ctx, entry.Code)
		return fmt.Errorf("download quota exceeded or entry expired")
	}
	if entry.BurnAfterReading {
		// burn 同样以本次计数成功为前提，仅在实际扣减后销毁
		if _, derr := s.DeleteEntry(ctx, entry.Code); derr != nil {
			return derr
		}
	}
	return s.LogAccess(ctx, entry.Code, "download", meta)
}

// claimDownloadSlot 为给定 IP 与 code 领取一个「逻辑下载」名额：
// 窗口内已领取则返回 false（视为并发/重复请求，不应重复计数）。
func (s *Service) claimDownloadSlot(ip, code string) bool {
	if strings.TrimSpace(ip) == "" {
		return true
	}
	key := ip + "|" + code
	now := time.Now().UnixMilli()
	s.dedupMu.Lock()
	defer s.dedupMu.Unlock()
	last, ok := s.downloadDedup[key]
	if ok && now-last < downloadDedupWindow.Milliseconds() {
		return false
	}
	s.downloadDedup[key] = now
	// 顺带清理过期键，避免 map 无限增长
	if len(s.downloadDedup) > 1024 {
		for k, t := range s.downloadDedup {
			if now-t >= downloadDedupWindow.Milliseconds() {
				delete(s.downloadDedup, k)
			}
		}
	}
	return true
}

func (s *Service) DeleteEntry(ctx context.Context, code string) (bool, error) {
	entry, err := s.GetEntry(ctx, code, true)
	if err != nil || entry == nil {
		return false, err
	}
	_ = s.getBackend(entry.StorageType).Delete(ctx, entry)
	db, err := s.open(ctx)
	if err != nil {
		return false, err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `DELETE FROM filebox_entries WHERE code = ?`, entry.Code)
	if err != nil {
		return false, fmt.Errorf("delete filebox entry: %w", err)
	}
	_ = s.LogAccess(context.Background(), entry.Code, "delete", requestMeta{})
	return true, nil
}

func (s *Service) GetAll(ctx context.Context) ([]PublicEntry, error) {
	if _, err := s.CleanupExpired(ctx); err != nil {
		return nil, err
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT code, type, content, original_name, filename, path, mimetype, size, created_at, expiry,
			burn_after_reading, downloads, max_downloads, access_password_hash, metadata_json,
			storage_type, server_id, remote_path
		FROM filebox_entries
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list filebox entries: %w", err)
	}
	defer rows.Close()
	entries := []PublicEntry{}
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, publicEntry(entry))
	}
	return entries, rows.Err()
}

func (s *Service) CleanupExpired(ctx context.Context) (int, error) {
	db, err := s.open(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT code FROM filebox_entries WHERE expiry > 0 AND expiry < ? AND deleted_at IS NULL`, time.Now().UnixMilli())
	if err != nil {
		_ = db.Close()
		return 0, fmt.Errorf("load expired filebox entries: %w", err)
	}
	codes := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			_ = rows.Close()
			_ = db.Close()
			return 0, err
		}
		codes = append(codes, code)
	}
	if err := rows.Close(); err != nil {
		_ = db.Close()
		return 0, err
	}
	_ = db.Close()
	for _, code := range codes {
		_, _ = s.DeleteEntry(ctx, code)
	}
	return len(codes), nil
}

func (s *Service) GetAccessLogs(ctx context.Context, code string, limitText string) ([]AccessLog, error) {
	limit := 100
	if parsed, err := strconv.Atoi(strings.TrimSpace(limitText)); err == nil && parsed > 0 {
		limit = parsed
	}
	if limit > maxAccessLogLimit {
		limit = maxAccessLogLimit
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	args := []interface{}{}
	where := ""
	if strings.TrimSpace(code) != "" {
		where = "WHERE code = ?"
		args = append(args, normalizeCode(code))
	}
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, `
		SELECT id, code, action, ip_address, user_agent, created_at
		FROM filebox_access_logs
		`+where+`
		ORDER BY created_at DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("load filebox access logs: %w", err)
	}
	defer rows.Close()
	logs := []AccessLog{}
	for rows.Next() {
		var log AccessLog
		var ip, userAgent sql.NullString
		if err := rows.Scan(&log.ID, &log.Code, &log.Action, &ip, &userAgent, &log.CreatedAt); err != nil {
			return nil, err
		}
		log.IPAddress = nullableStringPtr(ip)
		log.UserAgent = nullableStringPtr(userAgent)
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func (s *Service) LogAccess(ctx context.Context, code string, action string, meta requestMeta) error {
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		INSERT INTO filebox_access_logs (code, action, ip_address, user_agent)
		VALUES (?, ?, ?, ?)
	`, normalizeCode(code), action, nullString(meta.ip), nullString(meta.userAgent))
	if err != nil {
		return fmt.Errorf("log filebox access: %w", err)
	}
	return nil
}

func (s *Service) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.auth == nil {
		return true
	}
	ok, err := s.auth.IsAuthenticated(r.Context(), r)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !ok {
		response.JSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "please login first"})
		return false
	}
	return true
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	if err := s.ensureDirs(); err != nil {
		return nil, err
	}
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.schema.Ensure(func() error { return ensureSchema(ctx, db) }); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Service) ensureDirs() error {
	if err := os.MkdirAll(s.uploadsDir, 0o755); err != nil {
		return fmt.Errorf("create filebox upload dir: %w", err)
	}
	return nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS filebox_entries (
			code TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			content TEXT,
			original_name TEXT,
			filename TEXT NOT NULL,
			path TEXT,
			mimetype TEXT,
			size INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			expiry INTEGER NOT NULL,
			burn_after_reading INTEGER DEFAULT 0,
			downloads INTEGER DEFAULT 0,
			max_downloads INTEGER DEFAULT 0,
			access_password_hash TEXT,
			metadata_json TEXT,
			deleted_at INTEGER,
			storage_type TEXT DEFAULT 'local',
			server_id TEXT,
			remote_path TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS filebox_access_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT NOT NULL,
			action TEXT NOT NULL,
			ip_address TEXT,
			user_agent TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS filebox_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			max_file_size INTEGER NOT NULL DEFAULT 104857600,
			allowed_mime_types TEXT NOT NULL DEFAULT '[]',
			default_expiry_hours INTEGER NOT NULL DEFAULT 24,
			public_upload_enabled INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS filebox_void_rooms (
			id TEXT PRIMARY KEY,
			owner_token TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT 'persistent',
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL DEFAULT 0,
			deleted_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_filebox_entries_expiry ON filebox_entries(expiry)`,
		`CREATE INDEX IF NOT EXISTS idx_filebox_entries_created ON filebox_entries(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_filebox_access_code ON filebox_access_logs(code, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_filebox_void_rooms_deleted ON filebox_void_rooms(deleted_at, created_at)`,
		`INSERT OR IGNORE INTO filebox_settings (
			id, max_file_size, allowed_mime_types, default_expiry_hours, public_upload_enabled
		) VALUES (1, 104857600, '[]', 24, 0)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure filebox schema: %w", err)
		}
	}

	// 迁移已有表：补充 storage_type, server_id, remote_path 列
	existingColumns := make(map[string]bool)
	if rows, err := db.QueryContext(ctx, `PRAGMA table_info(filebox_entries)`); err == nil {
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
				existingColumns[strings.ToLower(name)] = true
			}
		}
		_ = rows.Close()
	}

	if !existingColumns["storage_type"] {
		_, _ = db.ExecContext(ctx, `ALTER TABLE filebox_entries ADD COLUMN storage_type TEXT DEFAULT 'local'`)
	}
	if !existingColumns["server_id"] {
		_, _ = db.ExecContext(ctx, `ALTER TABLE filebox_entries ADD COLUMN server_id TEXT`)
	}
	if !existingColumns["remote_path"] {
		_, _ = db.ExecContext(ctx, `ALTER TABLE filebox_entries ADD COLUMN remote_path TEXT`)
	}

	// 将历史遗留空值统一标记为 storage_type = 'local'
	_, _ = db.ExecContext(ctx, `UPDATE filebox_entries SET storage_type = 'local' WHERE storage_type IS NULL OR storage_type = ''`)

	return nil
}

func loadSettings(ctx context.Context, db *sql.DB) (Settings, error) {
	var row struct {
		maxFileSize      sql.NullInt64
		allowedMIMETypes sql.NullString
		defaultExpiry    sql.NullInt64
		publicUpload     sql.NullInt64
		updatedAt        sql.NullString
	}
	err := db.QueryRowContext(ctx, `
		SELECT max_file_size, allowed_mime_types, default_expiry_hours, public_upload_enabled, updated_at
		FROM filebox_settings
		WHERE id = 1
	`).Scan(&row.maxFileSize, &row.allowedMIMETypes, &row.defaultExpiry, &row.publicUpload, &row.updatedAt)
	if err != nil {
		return Settings{}, fmt.Errorf("load filebox settings: %w", err)
	}
	return Settings{
		MaxFileSize:         int64Default(row.maxFileSize, defaultMaxFileSize),
		AllowedMIMETypes:    parseStringArray(row.allowedMIMETypes.String),
		DefaultExpiryHours:  int(int64Default(row.defaultExpiry, defaultExpiryHours)),
		PublicUploadEnabled: row.publicUpload.Valid && row.publicUpload.Int64 == 1,
		UpdatedAt:           nullableStringPtr(row.updatedAt),
	}, nil
}

func findEntry(ctx context.Context, db *sql.DB, code string) (*Entry, error) {
	row := db.QueryRowContext(ctx, `
		SELECT code, type, content, original_name, filename, path, mimetype, size, created_at, expiry,
			burn_after_reading, downloads, max_downloads, access_password_hash, metadata_json,
			storage_type, server_id, remote_path
		FROM filebox_entries
		WHERE code = ? AND deleted_at IS NULL
	`, normalizeCode(code))
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}

type entryScanner interface {
	Scan(dest ...interface{}) error
}

func scanEntry(scanner entryScanner) (*Entry, error) {
	var entry Entry
	var content, originalName, pathValue, mimeType, passwordHash, metadataJSON sql.NullString
	var storageType, serverID, remotePath sql.NullString
	var size, createdAt, expiry, burnAfterReading, downloads, maxDownloads sql.NullInt64
	if err := scanner.Scan(
		&entry.Code,
		&entry.Type,
		&content,
		&originalName,
		&entry.Filename,
		&pathValue,
		&mimeType,
		&size,
		&createdAt,
		&expiry,
		&burnAfterReading,
		&downloads,
		&maxDownloads,
		&passwordHash,
		&metadataJSON,
		&storageType,
		&serverID,
		&remotePath,
	); err != nil {
		return nil, err
	}
	entry.Content = nullableStringPtr(content)
	entry.OriginalName = nullableStringPtr(originalName)
	entry.Path = nullableStringPtr(pathValue)
	entry.MIMEType = nullableStringPtr(mimeType)
	entry.Size = int64Default(size, 0)
	entry.CreatedAt = int64Default(createdAt, 0)
	entry.Expiry = int64Default(expiry, 0)
	entry.BurnAfterReading = burnAfterReading.Valid && burnAfterReading.Int64 == 1
	entry.Downloads = int64Default(downloads, 0)
	entry.MaxDownloads = int64Default(maxDownloads, 0)
	entry.AccessPasswordHash = nullableStringPtr(passwordHash)
	entry.RequiresPassword = passwordHash.Valid && passwordHash.String != ""
	entry.Metadata = parseObject(metadataJSON.String)
	entry.StorageType = "local"
	if storageType.Valid && storageType.String != "" {
		entry.StorageType = storageType.String
	}
	entry.ServerID = nullableStringPtr(serverID)
	entry.RemotePath = nullableStringPtr(remotePath)
	return &entry, nil
}

func publicEntry(entry *Entry) PublicEntry {
	result := PublicEntry{
		Code:             entry.Code,
		Type:             entry.Type,
		OriginalName:     entry.OriginalName,
		Filename:         entry.Filename,
		MIMEType:         entry.MIMEType,
		Size:             entry.Size,
		CreatedAt:        entry.CreatedAt,
		Expiry:           entry.Expiry,
		BurnAfterReading: entry.BurnAfterReading,
		Downloads:        entry.Downloads,
		MaxDownloads:     entry.MaxDownloads,
		RequiresPassword: entry.RequiresPassword,
		TextFormat:       entryTextFormat(entry),
		StorageType:      entry.StorageType,
		ServerID:         entry.ServerID,
	}
	if entry.Type == "text" && entry.Content != nil && !entry.RequiresPassword {
		runes := []rune(*entry.Content)
		if len(runes) > 80 {
			runes = runes[:80]
		}
		result.Preview = string(runes)
	}
	return result
}

func entryTextFormat(entry *Entry) string {
	if entry == nil || entry.Type != "text" {
		return ""
	}
	return textFormatMarkdown
}

func (s *Service) migrateJSONMetadata(ctx context.Context) error {
	if _, err := os.Stat(s.metadataFile); err != nil {
		return nil
	}
	bytes, err := os.ReadFile(s.metadataFile)
	if err != nil {
		return err
	}
	var raw map[string]map[string]interface{}
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	db, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO filebox_entries (
			code, type, content, original_name, filename, path, mimetype, size, created_at, expiry,
			burn_after_reading, downloads, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, item := range raw {
		code := normalizeCode(fmt.Sprint(item["code"]))
		if code == "" {
			continue
		}
		encoded, _ := json.Marshal(item)
		_, err = stmt.ExecContext(
			ctx,
			code,
			emptyDefault(fmt.Sprint(item["type"]), "file"),
			nullStringFromAny(item["content"]),
			nullStringFromAny(item["originalName"]),
			emptyDefault(fmt.Sprint(item["filename"]), "file_"+code),
			nullStringFromAny(item["path"]),
			nullStringFromAny(item["mimetype"]),
			numberFromAny(item["size"]),
			numberFromAny(item["createdAt"]),
			numberFromAny(item["expiry"]),
			boolInt(parseBool(item["burnAfterReading"])),
			numberFromAny(item["downloads"]),
			string(encoded),
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func parseShareRequest(w http.ResponseWriter, r *http.Request, maxFileSize int64) (sharePayload, *multipart.FileHeader, error) {
	// 流式请求体上限：先用 MaxBytesReader 卡住超限上传（不落盘），
	// 再按 multipart 声明继续校验，双重防线。
	if maxFileSize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+(1<<20))
	}
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "application/json") {
		defer r.Body.Close()
		var payload sharePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return sharePayload{}, nil, errors.New("request parameter validation failed")
		}
		return payload, nil, nil
	}
	if err := r.ParseMultipartForm(multipartMemoryBudget); err != nil {
		return sharePayload{}, nil, err
	}
	payload := sharePayload{
		Type:           r.FormValue("type"),
		Text:           r.FormValue("text"),
		Expiry:         r.FormValue("expiry"),
		MaxDownloads:   r.FormValue("max_downloads"),
		AccessPassword: r.FormValue("access_password"),
		Password:       r.FormValue("password"),
	}
	if values, ok := r.MultipartForm.Value["burn_after_reading"]; ok && len(values) > 0 {
		payload.BurnAfterReading = values[0]
	}
	file, header, err := r.FormFile("file")
	if err == nil {
		_ = file.Close()
		if header.Size > maxFileSize {
			return sharePayload{}, nil, fmt.Errorf("file too large, max %d bytes", maxFileSize)
		}
		return payload, header, nil
	}
	if errors.Is(err, http.ErrMissingFile) {
		return payload, nil, nil
	}
	return sharePayload{}, nil, err
}

func accessPasswordFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.URL.Query().Get("password")); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.Header.Get("X-Filebox-Password")); value != "" {
		return value
	}
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "application/json") && r.Body != nil {
		var body struct {
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		return body.Password
	}
	return ""
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "request parameter validation failed"})
		return false
	}
	return true
}

func metaFromRequest(r *http.Request) requestMeta {
	ip := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if idx := strings.Index(ip, ","); idx >= 0 {
		ip = strings.TrimSpace(ip[:idx])
	}
	if ip == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			ip = host
		} else {
			ip = r.RemoteAddr
		}
	}
	return requestMeta{ip: ip, userAgent: r.UserAgent()}
}

func hashAccessPassword(accessPassword string) (*string, error) {
	if strings.TrimSpace(accessPassword) == "" {
		return nil, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(accessPassword), 10)
	if err != nil {
		return nil, fmt.Errorf("hash filebox access password: %w", err)
	}
	value := string(hash)
	return &value, nil
}

func verifyAccessPassword(entry *Entry, accessPassword string) bool {
	if entry == nil || entry.AccessPasswordHash == nil || *entry.AccessPasswordHash == "" {
		return true
	}
	if accessPassword == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(*entry.AccessPasswordHash), []byte(accessPassword)) == nil
}

// randomCode 用拒绝采样从 codeAlphabet 均匀取字符：直接对随机字节取模，
// 在字母表长度不整除 256 时会让靠前字符出现概率偏高；先丢弃落在有效区间
// 之外的字节再取模，保证每个字符等概率。
func randomCode(length int) (string, error) {
	alphabetLen := len(codeAlphabet)
	// 用 int 计算 256 内最大的可接受字节数，避免 byte 溢出；
	// 字母表长度整除 256 时等于 256，即不丢弃任何字节。
	maxValid := 256 / alphabetLen * alphabetLen
	var builder strings.Builder
	builder.Grow(length)
	buf := make([]byte, length)
	for builder.Len() < length {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate filebox code: %w", err)
		}
		for _, value := range buf {
			if int(value) >= maxValid {
				continue
			}
			builder.WriteByte(codeAlphabet[int(value)%alphabetLen])
			if builder.Len() == length {
				break
			}
		}
	}
	return builder.String(), nil
}

func parseExpiryHours(value string, fallback int) float64 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "0" || strings.EqualFold(trimmed, "permanent") || strings.EqualFold(trimmed, "forever") {
		return 0
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || parsed <= 0 {
		return float64(fallback)
	}
	return parsed
}

// maxShareExpiryHours 分享有效期上限（100 年）：防止超大值在 float→int64
// 换算中溢出成负数，导致 isExpired 恒假而变成「永久分享」。
const maxShareExpiryHours = 876000.0

func expiryTime(now int64, expiryHours float64) int64 {
	if expiryHours <= 0 {
		return 0
	}
	if expiryHours > maxShareExpiryHours {
		expiryHours = maxShareExpiryHours
	}
	return now + int64(expiryHours*float64(time.Hour/time.Millisecond))
}

func isExpired(expiry int64) bool {
	return expiry > 0 && time.Now().UnixMilli() > expiry
}

func parseNonNegativeInt64(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func parseBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true") || typed == "1"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func sanitizeFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "upload.bin"
	}
	replacer := strings.NewReplacer("<", "_", ">", "_", ":", "_", "\"", "_", "/", "_", "\\", "_", "|", "_", "?", "_", "*", "_")
	base = replacer.Replace(base)
	if len(base) > 180 {
		base = base[:180]
	}
	return base
}

func saveUploadedFile(header *multipart.FileHeader, target string) error {
	src, err := header.Open()
	if err != nil {
		return fmt.Errorf("open uploaded file: %w", err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create uploaded file: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("save uploaded file: %w", err)
	}
	return nil
}

func isPathInside(root string, candidate string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func isMIMEAllowed(mimeType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	mimeValue := strings.ToLower(strings.TrimSpace(mimeType))
	for _, pattern := range allowed {
		rule := strings.ToLower(strings.TrimSpace(pattern))
		if rule == "" {
			continue
		}
		if strings.HasSuffix(rule, "/*") && strings.HasPrefix(mimeValue, strings.TrimSuffix(rule, "*")) {
			return true
		}
		if mimeValue == rule {
			return true
		}
	}
	return false
}

func positiveInt64(value interface{}, fallback int64) int64 {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return int64(typed)
		}
	case int:
		if typed > 0 {
			return int64(typed)
		}
	case int64:
		if typed > 0 {
			return typed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func stringSlice(value interface{}, fallback []string) []string {
	raw, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]string); ok {
			return cleanStrings(typed)
		}
		return fallback
	}
	result := []string{}
	for _, item := range raw {
		result = append(result, fmt.Sprint(item))
	}
	return cleanStrings(result)
}

func cleanStrings(values []string) []string {
	result := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func boolValue(value interface{}, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return parseBool(value)
}

func parseStringArray(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	var result []string
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return []string{}
	}
	return result
}

func parseObject(value string) map[string]interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil
	}
	return result
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func int64Default(value sql.NullInt64, fallback int64) int64 {
	if value.Valid {
		return value.Int64
	}
	return fallback
}

func nullString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullStringFromAny(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return nil
	}
	return text
}

func numberFromAny(value interface{}) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" || value == "<nil>" {
		return fallback
	}
	return value
}

func (s *Service) listStorageNodes(w http.ResponseWriter, r *http.Request) {
	if s.nodeProvider == nil {
		response.OK(w, []StorageNodeInfo{})
		return
	}
	nodes, err := s.nodeProvider.ListEligibleStorageNodes(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, nodes)
}

func (s *Service) initRemoteUpload(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ServerID string `json:"serverId"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	payload.ServerID = strings.TrimSpace(payload.ServerID)
	payload.Filename = sanitizeFilename(strings.TrimSpace(payload.Filename))
	if payload.ServerID == "" || payload.Filename == "" || payload.Size <= 0 {
		response.Error(w, http.StatusBadRequest, "serverId, filename, and positive size required")
		return
	}
	if s.nodeProvider == nil {
		response.Error(w, http.StatusServiceUnavailable, "node storage provider not configured")
		return
	}
	node, err := s.nodeProvider.GetEligibleStorageNode(r.Context(), payload.ServerID)
	if err != nil {
		response.Error(w, http.StatusBadRequest, fmt.Sprintf("invalid storage node: %v", err))
		return
	}
	key, err := s.nodeProvider.GetStorageNodeAgentKey(r.Context(), payload.ServerID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to get agent credentials")
		return
	}

	code, err := s.GenerateCode(r.Context(), defaultCodeLength)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	uploadURL, err := BuildSignedURL("PUT", node.Host, node.StoragePort, code, payload.Filename, payload.Size, 15*time.Minute, key)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to build upload URL: %v", err))
		return
	}

	response.OK(w, map[string]interface{}{
		"code":      code,
		"filename":  payload.Filename,
		"uploadUrl": uploadURL,
		"expires":   time.Now().Add(15 * time.Minute).Unix(),
	})
}

func (s *Service) completeRemoteUpload(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Code             string  `json:"code"`
		Filename         string  `json:"filename"`
		Size             int64   `json:"size"`
		ServerID         string  `json:"serverId"`
		MIMEType         string  `json:"mimeType"`
		Expiry           string  `json:"expiry"`
		BurnAfterReading any     `json:"burn_after_reading"`
		MaxDownloads     string  `json:"max_downloads"`
		AccessPassword   string  `json:"access_password"`
		Password         string  `json:"password"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	code := normalizeCode(payload.Code)
	filename := sanitizeFilename(payload.Filename)
	serverID := strings.TrimSpace(payload.ServerID)
	if code == "" || filename == "" || serverID == "" {
		response.Error(w, http.StatusBadRequest, "code, filename, and serverId required")
		return
	}
	if s.nodeProvider == nil {
		response.Error(w, http.StatusServiceUnavailable, "node storage provider not configured")
		return
	}
	_, err := s.nodeProvider.GetEligibleStorageNode(r.Context(), serverID)
	if err != nil {
		response.Error(w, http.StatusBadRequest, fmt.Sprintf("storage node unavailable: %v", err))
		return
	}

	settings, err := s.LoadSettings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	expiryHours := parseExpiryHours(payload.Expiry, settings.DefaultExpiryHours)
	burn := parseBool(payload.BurnAfterReading)
	maxDownloads := parseNonNegativeInt64(payload.MaxDownloads)
	accessPassword := payload.AccessPassword
	if accessPassword == "" {
		accessPassword = payload.Password
	}

	entry, err := s.AddRemoteFile(r.Context(), code, filename, payload.Size, payload.MIMEType, serverID, expiryHours, burn, maxDownloads, accessPassword)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, publicEntry(entry))
}

func (s *Service) transferStorage(w http.ResponseWriter, r *http.Request, code string) {
	var payload struct {
		TargetStorageType string `json:"targetStorageType"` // "local" or "remote"
		TargetServerID    string `json:"targetServerId"`    // required if remote
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	targetType := strings.ToLower(strings.TrimSpace(payload.TargetStorageType))
	if targetType != "local" && targetType != "remote" {
		response.Error(w, http.StatusBadRequest, "targetStorageType must be 'local' or 'remote'")
		return
	}

	entry, err := s.GetEntry(r.Context(), code, true)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		response.Error(w, http.StatusNotFound, "share not found")
		return
	}
	if entry.Type != "file" {
		response.Error(w, http.StatusBadRequest, "only file shares can be transferred")
		return
	}

	// 相同存储目标直接返回
	if entry.StorageType == targetType {
		if targetType == "local" {
			response.OK(w, publicEntry(entry))
			return
		}
		if entry.ServerID != nil && *entry.ServerID == payload.TargetServerID {
			response.OK(w, publicEntry(entry))
			return
		}
	}

	client := &http.Client{Timeout: 60 * time.Second}

	// 1. 获取源文件的读取流或临时文件
	var srcReader io.ReadCloser
	var srcSize int64 = entry.Size

	if entry.StorageType == "local" {
		if entry.Path == nil || *entry.Path == "" {
			response.Error(w, http.StatusInternalServerError, "source local file path missing")
			return
		}
		file, err := os.Open(*entry.Path)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to open local source: %v", err))
			return
		}
		defer file.Close()
		srcReader = file
	} else {
		// remote source
		if s.nodeProvider == nil || entry.ServerID == nil || *entry.ServerID == "" {
			response.Error(w, http.StatusServiceUnavailable, "source storage node provider unavailable")
			return
		}
		srcNode, err := s.nodeProvider.GetEligibleStorageNode(r.Context(), *entry.ServerID)
		if err != nil {
			response.Error(w, http.StatusBadRequest, fmt.Sprintf("source storage node unavailable: %v", err))
			return
		}
		srcKey, err := s.nodeProvider.GetStorageNodeAgentKey(r.Context(), *entry.ServerID)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "failed to get source agent key")
			return
		}
		getURL, err := BuildSignedURL("GET", srcNode.Host, srcNode.StoragePort, entry.Code, entry.Filename, 0, 10*time.Minute, srcKey)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "failed to build source download URL")
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, getURL, nil)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				_ = resp.Body.Close()
			}
			response.Error(w, http.StatusBadGateway, "failed to download from source remote node")
			return
		}
		defer resp.Body.Close()
		srcReader = resp.Body
	}

	// 为确保一致性（size 与 sha256 校验），先写入本地临时文件
	tmpFile, err := os.CreateTemp(s.uploadsDir, "transfer-*.tmp")
	if err != nil {
		response.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to create temp file: %v", err))
		return
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	hasher := sha256.New()
	multiWriter := io.MultiWriter(tmpFile, hasher)
	copiedBytes, err := io.Copy(multiWriter, srcReader)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to buffer source file: %v", err))
		return
	}
	_ = tmpFile.Close()

	if srcSize > 0 && copiedBytes != srcSize {
		response.Error(w, http.StatusBadGateway, fmt.Sprintf("size mismatch during transfer: expected %d, got %d", srcSize, copiedBytes))
		return
	}

	// 2. 写入新目标
	var newPath *string
	var newServerID *string
	var newRemotePath *string

	if targetType == "local" {
		safeName := sanitizeFilename(entry.Filename)
		saveFilename := fmt.Sprintf("%d-%s-%s", time.Now().UnixMilli(), entry.Code, safeName)
		finalLocalPath := filepath.Join(s.uploadsDir, saveFilename)
		if err := os.Rename(tmpPath, finalLocalPath); err != nil {
			// fallback copy
			if copyErr := copyFileContents(tmpPath, finalLocalPath); copyErr != nil {
				response.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to write target local file: %v", copyErr))
				return
			}
		}
		newPath = &finalLocalPath
	} else {
		// remote target
		targetServerID := strings.TrimSpace(payload.TargetServerID)
		if s.nodeProvider == nil {
			response.Error(w, http.StatusServiceUnavailable, "storage node provider unavailable")
			return
		}
		targetNode, err := s.nodeProvider.GetEligibleStorageNode(r.Context(), targetServerID)
		if err != nil {
			response.Error(w, http.StatusBadRequest, fmt.Sprintf("target storage node invalid or ineligible: %v", err))
			return
		}
		targetKey, err := s.nodeProvider.GetStorageNodeAgentKey(r.Context(), targetServerID)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "failed to get target agent key")
			return
		}

		putURL, err := BuildSignedURL("PUT", targetNode.Host, targetNode.StoragePort, entry.Code, entry.Filename, copiedBytes, 10*time.Minute, targetKey)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "failed to build target upload URL")
			return
		}

		uploadStream, err := os.Open(tmpPath)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "failed to open buffered file for upload")
			return
		}
		defer uploadStream.Close()

		putReq, err := http.NewRequestWithContext(r.Context(), http.MethodPut, putURL, uploadStream)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		putReq.ContentLength = copiedBytes
		putReq.Header.Set("Content-Type", "application/octet-stream")

		putResp, err := client.Do(putReq)
		if err != nil || putResp.StatusCode != http.StatusOK {
			statusMsg := ""
			if putResp != nil {
				_ = putResp.Body.Close()
				statusMsg = fmt.Sprintf(" (HTTP %d)", putResp.StatusCode)
			}
			response.Error(w, http.StatusBadGateway, fmt.Sprintf("failed to upload to target node%s", statusMsg))
			return
		}
		_ = putResp.Body.Close()

		newServerID = &targetServerID
		rp := fmt.Sprintf("shares/%s/%s", entry.Code, entry.Filename)
		newRemotePath = &rp
	}

	// 3. 更新数据库记录
	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	_, err = db.ExecContext(r.Context(), `
		UPDATE filebox_entries
		SET storage_type = ?,
			server_id = ?,
			path = ?,
			remote_path = ?,
			size = ?
		WHERE code = ?
	`, targetType, newServerID, newPath, newRemotePath, copiedBytes, entry.Code)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, fmt.Sprintf("failed to update entry metadata: %v", err))
		return
	}

	// 4. 清理旧目标存储中的文件
	_ = s.getBackend(entry.StorageType).Delete(r.Context(), entry)

	updatedEntry, err := s.GetEntry(r.Context(), entry.Code, true)
	if err != nil || updatedEntry == nil {
		response.OK(w, map[string]interface{}{"success": true})
		return
	}
	response.OK(w, publicEntry(updatedEntry))
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// HandleShareRedirect 拦截外部直接访问 /share/{code} 的直链下载：
// 若为远程存储且无需密码，原子扣减次数后 302 重定向至节点直链；
// 其他情况（如需要密码、本地文件、文本）返回 false 交由前端 SPA 渲染。
func (s *Service) HandleShareRedirect(w http.ResponseWriter, r *http.Request, code string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	entry, err := s.GetEntry(r.Context(), code, false)
	if err != nil || entry == nil {
		return false
	}
	// 仅限远端存储且无需密码的文件分享触发直链分发
	if entry.StorageType != "remote" || entry.Type != "file" || entry.RequiresPassword {
		return false
	}
	if s.nodeProvider == nil || entry.ServerID == nil || *entry.ServerID == "" {
		return false
	}
	node, err := s.nodeProvider.GetEligibleStorageNode(r.Context(), *entry.ServerID)
	if err != nil {
		return false
	}
	key, err := s.nodeProvider.GetStorageNodeAgentKey(r.Context(), *entry.ServerID)
	if err != nil {
		return false
	}

	// 扣除下载配额或触发阅后即焚
	if err := s.AccessEntry(r.Context(), entry.Code, metaFromRequest(r)); err != nil {
		http.Error(w, "Download quota exceeded or entry expired", http.StatusForbidden)
		return true
	}

	signedURL, err := BuildSignedURL("GET", node.Host, node.StoragePort, entry.Code, entry.Filename, 0, 5*time.Minute, key)
	if err != nil {
		http.Error(w, "Failed to build signed URL", http.StatusInternalServerError)
		return true
	}

	http.Redirect(w, r, signedURL, http.StatusFound)
	return true
}
