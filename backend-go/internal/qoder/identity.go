package qoder

import (
	"crypto/md5"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

const (
	legacyClientType = "5"
	newClientType    = "10"
	// newCosyVersion 是「未配置真实指纹、但想知道新版协议默认值」时的兜底版本。
	// 配了指纹的账号优先用指纹里的 version，其次用区域客户端版本（regionConfig.clientVersion）。
	newCosyVersion   = "0.4.8"
	legacyUA         = "Go-http-client/2.0"
	newUA            = "undici"
	defaultMachineOS = "aarch64_darwin"
)

type CheckinFingerprint struct {
	MachineID    string `json:"machine_id"`
	MachineToken string `json:"machine_token"`
	MachineType  string `json:"machine_type"`
	ClientIP     string `json:"client_ip,omitempty"`
	ClientType   string `json:"client_type,omitempty"`
	Version      string `json:"version,omitempty"`
	MachineOS    string `json:"machine_os,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	MachineCode  string `json:"machine_code,omitempty"`
	Scene        string `json:"scene,omitempty"`
	Product      string `json:"product,omitempty"`
	DataPolicy   string `json:"data_policy,omitempty"`
}

func (f *CheckinFingerprint) real() bool {
	return f != nil && strings.TrimSpace(f.MachineToken) != ""
}

type deviceIdentity struct {
	MachineID    string
	MachineToken string
	MachineType  string
	ClientType   string
	Version      string
	Hostname     string
	MachineOS    string
	MachineCode  string
	ClientIP     string
	Scene        string
	Product      string
	DataPolicy   string
	Real         bool
}

func (id deviceIdentity) modernProtocol() bool {
	return id.Real
}

func identityForAccount(acc Account) deviceIdentity {
	if fp := acc.CheckinFingerprint; fp.real() {
		cfg := regionConfigFor(acc.Region)
		return deviceIdentity{
			MachineID:    strings.TrimSpace(fp.MachineID),
			MachineToken: strings.TrimSpace(fp.MachineToken),
			MachineType:  strings.TrimSpace(fp.MachineType),
			ClientType:   firstNonEmpty(strings.TrimSpace(fp.ClientType), newClientType),
			// version 优先用抓包/提取到的真实客户端版本，否则用该区域客户端当前版本。
			Version:     firstNonEmpty(strings.TrimSpace(fp.Version), cfg.clientVersion, newCosyVersion),
			Hostname:    strings.TrimSpace(fp.Hostname),
			MachineOS:   firstNonEmpty(strings.TrimSpace(fp.MachineOS), defaultMachineOS),
			MachineCode: strings.TrimSpace(fp.MachineCode),
			ClientIP:    strings.TrimSpace(fp.ClientIP),
			Scene:       firstNonEmpty(strings.TrimSpace(fp.Scene), "app"),
			Product:     firstNonEmpty(strings.TrimSpace(fp.Product), "app"),
			DataPolicy:  firstNonEmpty(strings.TrimSpace(fp.DataPolicy), "agree"),
			Real:        true,
		}
	}
	seed := firstNonEmpty(strings.TrimSpace(acc.ID), strings.TrimSpace(acc.UserID), acc.AccessToken)
	return deviceIdentity{
		MachineID:    deriveMachineID(seed),
		MachineToken: deriveMachineToken(seed),
		MachineType:  deriveMachineType(seed),
		ClientType:   legacyClientType,
		Version:      legacyCosyVersion,
		Hostname:     deriveMachineHostname(seed),
		MachineOS:    defaultMachineOS,
		ClientIP:     "169.254.198.161",
	}
}

func deriveMachineID(seed string) string {
	sum := md5.Sum([]byte("machine:" + seed))
	return hexToUUIDv4(fmt.Sprintf("%x", sum))
}

func deriveMachineType(seed string) string {
	sum := md5.Sum([]byte("machinetype:" + seed))
	return fmt.Sprintf("%x", sum)[:18]
}

func deriveMachineToken(seed string) string {
	sum := sha512.Sum512([]byte("machinetoken:" + seed))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:43]
}

func deriveMachineHostname(seed string) string {
	sum := md5.Sum([]byte("hostname:" + seed))
	return "DESKTOP-" + strings.ToUpper(fmt.Sprintf("%x", sum)[:7])
}

func hexToUUIDv4(hex string) string {
	if len(hex) < 32 {
		return hex
	}
	b := []byte(hex[:32])
	b[12] = '4'
	b[16] = 'a'
	return fmt.Sprintf("%s-%s-%s-%s-%s", b[0:8], b[8:12], b[12:16], b[16:20], b[20:32])
}

func (id deviceIdentity) deviceHeaders() map[string]string {
	h := map[string]string{
		"cosy-clienttype":   id.ClientType,
		"cosy-version":      id.Version,
		"cosy-machineid":    id.MachineID,
		"cosy-machinetoken": id.MachineToken,
		"cosy-machinetype":  id.MachineType,
	}
	if id.MachineCode != "" {
		h["cosy-machinecode"] = id.MachineCode
	}
	if id.Hostname != "" {
		h["cosy-machinehostname"] = id.Hostname
	}
	if id.MachineOS != "" {
		h["cosy-machineos"] = id.MachineOS
	}
	return h
}

func (id deviceIdentity) userAgent() string {
	if id.modernProtocol() {
		return newUA
	}
	return legacyUA
}

func applyHeaders(dst map[string]string, src map[string]string) {
	for k, v := range src {
		if v != "" {
			dst[k] = v
		}
	}
}

// applySignedHeaders 施加 COSY 签名请求（对话 / 模型目录）的公共头。
// 新版协议（有真实指纹）额外带 business-product / scene / data-policy 与真实
// clientip，并把 UA / clienttype / version 换成官方桌面端形态；旧协议保持原样。
// 签名本身（authorization / cosy-key / cosy-date / x-model-key）由调用方设置。
func (id deviceIdentity) applySignedHeaders(req *http.Request, cosyDate string, cosyKey string) {
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("accept-encoding", "identity")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cosy-key", cosyKey)
	req.Header.Set("cosy-date", cosyDate)
	req.Header.Set("login-version", "v2")
	req.Header.Set("user-agent", id.userAgent())
	for k, v := range id.deviceHeaders() {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	if id.modernProtocol() {
		req.Header.Set("cosy-data-policy", id.DataPolicy)
		req.Header.Set("cosy-business-product", id.Product)
		req.Header.Set("cosy-scene", id.Scene)
		req.Header.Set("cosy-clientip", firstNonEmpty(id.ClientIP, id.MachineID))
	} else {
		req.Header.Set("cosy-data-policy", "AGREE")
		req.Header.Set("cosy-clientip", firstNonEmpty(id.ClientIP, "169.254.198.161"))
	}
}
