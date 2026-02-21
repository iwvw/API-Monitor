package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// 文件管理任务类型常量
const (
	TaskTypeFileList   = 30 // 列出目录
	TaskTypeFileRead   = 31 // 读取文件
	TaskTypeFileWrite  = 32 // 写入文件
	TaskTypeFileMkdir  = 33 // 创建目录
	TaskTypeFileDelete = 34 // 删除文件/目录
	TaskTypeFileRename        = 35 // 重命名/移动
	TaskTypeFileStat          = 36 // 获取文件信息
	TaskTypeFileChmod         = 37 // 修改权限
	TaskTypeFileDownloadChunk = 38 // 下载文件块
)

// FileDownloadChunkRequest 读取文件分块请求
type FileDownloadChunkRequest struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
	Size   int    `json:"size"`
}

// FileListRequest 目录列表请求
type FileListRequest struct {
	Path string `json:"path"`
}

// FileEntry 文件信息条目
type FileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	IsDirectory bool   `json:"isDirectory"`
	IsFile      bool   `json:"isFile"`
	IsSymlink   bool   `json:"isSymlink"`
	Size        int64  `json:"size"`
	Mode        uint32 `json:"mode"`
	Mtime       int64  `json:"mtime"`
	Atime       int64  `json:"atime"`
	Permissions string `json:"permissions"`
}

// FileListResponse 目录列表响应
type FileListResponse struct {
	Files []FileEntry `json:"files"`
	Cwd   string      `json:"cwd"`
}

// FileReadRequest 读取文件请求
type FileReadRequest struct {
	Path    string `json:"path"`
	MaxSize int64  `json:"maxSize"`
}

// FileWriteRequest 写入文件请求
type FileWriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FileMkdirRequest 创建目录请求
type FileMkdirRequest struct {
	Path string `json:"path"`
}

// FileDeleteRequest 删除请求
type FileDeleteRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

// FileRenameRequest 重命名请求
type FileRenameRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

// FileStatRequest 文件信息请求
type FileStatRequest struct {
	Path string `json:"path"`
}

// FileChmodRequest 修改权限请求
type FileChmodRequest struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode"`
}

// normalizePath 统一路径格式（使用正斜杠，SFTP 兼容）
func normalizePath(p string) string {
	// 将反斜杠统一转为正斜杠
	return strings.ReplaceAll(p, "\\", "/")
}

// resolvePath 解析实际路径（处理 . 和相对路径）
func resolvePath(inputPath string) (string, error) {
	if inputPath == "" || inputPath == "." {
		// 返回用户主目录
		home, err := os.UserHomeDir()
		if err != nil {
			// 回退到当前工作目录
			cwd, err := os.Getwd()
			if err != nil {
				return "/", nil
			}
			return cwd, nil
		}
		return home, nil
	}

	// Windows 虚拟根目录: "/" 表示列出所有盘符
	if runtime.GOOS == "windows" && (inputPath == "/" || inputPath == "\\") {
		return "/", nil // 特殊标记，handleFileList 会处理
	}

	// 统一将反斜杠转为正斜杠
	p := strings.ReplaceAll(inputPath, "\\", "/")

	// 修复 SFTP 风格路径：/C:/xxx → C:/xxx（Windows 专用）
	// filepath.Abs("/C:/Windows") 在 Windows 上会错误解析为 C:\C:\Windows
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:] // 去掉开头的 /
	}

	// Windows: 处理裸盘符路径 "C:" → "C:/"
	// filepath.Abs("C:") 返回的是当前工作目录而非驱动器根目录
	if runtime.GOOS == "windows" && len(p) == 2 && p[1] == ':' {
		p = p + "/"
	}

	// 解析为绝对路径
	absPath, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return absPath, nil
}

// listWindowsDrives 列出 Windows 系统上的所有可用盘符
func listWindowsDrives() []FileEntry {
	drives := make([]FileEntry, 0)
	for letter := 'A'; letter <= 'Z'; letter++ {
		drivePath := fmt.Sprintf("%c:\\", letter)
		_, err := os.Stat(drivePath)
		if err == nil {
			name := fmt.Sprintf("%c:", letter)
			drives = append(drives, FileEntry{
				Name:        name,
				Path:        fmt.Sprintf("%c:/", letter),
				IsDirectory: true,
				IsFile:      false,
				IsSymlink:   false,
				Size:        0,
				Mode:        0755,
				Mtime:       0,
				Atime:       0,
				Permissions: "drwxr-xr-x",
			})
		}
	}
	return drives
}

// formatPermissions 格式化权限位为 rwx 形式
func formatPermissions(mode fs.FileMode) string {
	perms := []string{"---", "--x", "-w-", "-wx", "r--", "r-x", "rw-", "rwx"}

	owner := perms[(mode>>6)&7]
	group := perms[(mode>>3)&7]
	other := perms[mode&7]

	var typeChar string
	if mode.IsDir() {
		typeChar = "d"
	} else if mode&fs.ModeSymlink != 0 {
		typeChar = "l"
	} else {
		typeChar = "-"
	}

	return typeChar + owner + group + other
}

// handleFileList 处理文件列表请求
func (a *AgentClient) handleFileList(data string) (result string, err error) {
	// 防止 panic 导致整个 Agent 崩溃
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("内部错误: %v", r)
		}
	}()

	var req FileListRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	absPath, err := resolvePath(req.Path)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %v", err)
	}

	// Windows 虚拟根目录：列出所有盘符
	if runtime.GOOS == "windows" && absPath == "/" {
		resp := FileListResponse{
			Files: listWindowsDrives(),
			Cwd:   "/",
		}
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return "", fmt.Errorf("序列化响应失败: %v", err)
		}
		return string(jsonBytes), nil
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", fmt.Errorf("读取目录失败: %v", err)
	}

	files := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fullPath := filepath.Join(absPath, entry.Name())
		fileType := entry.Type()

		fe := FileEntry{
			Name:        entry.Name(),
			Path:        normalizePath(fullPath),
			IsDirectory: entry.IsDir(),
			IsFile:      info.Mode().IsRegular(),
			IsSymlink:   fileType&fs.ModeSymlink != 0,
			Size:        info.Size(),
			Mode:        uint32(info.Mode().Perm()),
			Mtime:       info.ModTime().UnixMilli(),
			Atime:       info.ModTime().UnixMilli(),
			Permissions: formatPermissions(info.Mode()),
		}
		files = append(files, fe)
	}

	resp := FileListResponse{
		Files: files,
		Cwd:   normalizePath(absPath),
	}

	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("序列化响应失败: %v", err)
	}

	return string(jsonBytes), nil
}

// handleFileRead 处理文件读取请求
func (a *AgentClient) handleFileRead(data string) (string, error) {
	var req FileReadRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.Path == "" {
		return "", fmt.Errorf("文件路径不能为空")
	}

	// 检查文件大小
	info, err := os.Stat(req.Path)
	if err != nil {
		return "", fmt.Errorf("获取文件信息失败: %v", err)
	}

	maxSize := req.MaxSize
	if maxSize <= 0 {
		maxSize = 2 * 1024 * 1024 // 默认最大 2MB
	}

	if info.Size() > maxSize {
		return "", fmt.Errorf("文件过大 (%d bytes), 最大允许 %d bytes", info.Size(), maxSize)
	}

	content, err := os.ReadFile(req.Path)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %v", err)
	}

	return string(content), nil
}

// handleFileWrite 处理文件写入请求
func (a *AgentClient) handleFileWrite(data string) (string, error) {
	var req FileWriteRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.Path == "" {
		return "", fmt.Errorf("文件路径不能为空")
	}

	absPath, err := resolvePath(req.Path)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %v", err)
	}

	// 确保父目录存在
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建父目录失败: %v", err)
	}

	if err := os.WriteFile(absPath, []byte(req.Content), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %v", err)
	}

	return "文件保存成功", nil
}

// handleFileMkdir 处理创建目录请求
func (a *AgentClient) handleFileMkdir(data string) (string, error) {
	var req FileMkdirRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.Path == "" {
		return "", fmt.Errorf("目录路径不能为空")
	}

	absPath, err := resolvePath(req.Path)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %v", err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %v", err)
	}

	return "目录创建成功", nil
}

// handleFileDelete 处理删除请求
func (a *AgentClient) handleFileDelete(data string) (string, error) {
	var req FileDeleteRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.Path == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	absPath, err := resolvePath(req.Path)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %v", err)
	}

	// 安全检查：禁止删除根目录
	cleanPath := filepath.Clean(absPath)
	if cleanPath == "/" || cleanPath == "\\" {
		return "", fmt.Errorf("不允许删除根目录")
	}
	if runtime.GOOS == "windows" {
		// 检查是否为驱动器根目录 (如 C:\)
		if len(cleanPath) <= 3 && strings.HasSuffix(cleanPath, "\\") {
			return "", fmt.Errorf("不允许删除驱动器根目录")
		}
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("文件不存在: %v", err)
	}

	if info.IsDir() {
		if req.Recursive {
			if err := os.RemoveAll(absPath); err != nil {
				return "", fmt.Errorf("删除目录失败: %v", err)
			}
			return "目录已删除", nil
		}
		if err := os.Remove(absPath); err != nil {
			return "", fmt.Errorf("删除空目录失败 (如需递归删除请设置 recursive=true): %v", err)
		}
		return "目录已删除", nil
	}

	if err := os.Remove(absPath); err != nil {
		return "", fmt.Errorf("删除文件失败: %v", err)
	}
	return "文件已删除", nil
}

// handleFileRename 处理重命名/移动请求
func (a *AgentClient) handleFileRename(data string) (string, error) {
	var req FileRenameRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.OldPath == "" || req.NewPath == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	oldAbsPath, err := resolvePath(req.OldPath)
	if err != nil {
		return "", fmt.Errorf("解析源路径失败: %v", err)
	}

	newAbsPath, err := resolvePath(req.NewPath)
	if err != nil {
		return "", fmt.Errorf("解析目标路径失败: %v", err)
	}

	if err := os.Rename(oldAbsPath, newAbsPath); err != nil {
		return "", fmt.Errorf("重命名失败: %v", err)
	}

	return "重命名成功", nil
}

// handleFileStat 处理文件信息请求
func (a *AgentClient) handleFileStat(data string) (string, error) {
	var req FileStatRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.Path == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	absPath, err := resolvePath(req.Path)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %v", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("获取文件信息失败: %v", err)
	}

	entry := FileEntry{
		Name:        info.Name(),
		Path:        normalizePath(absPath),
		IsDirectory: info.IsDir(),
		IsFile:      info.Mode().IsRegular(),
		IsSymlink:   info.Mode()&fs.ModeSymlink != 0,
		Size:        info.Size(),
		Mode:        uint32(info.Mode().Perm()),
		Mtime:       info.ModTime().UnixMilli(),
		Atime:       info.ModTime().UnixMilli(),
		Permissions: formatPermissions(info.Mode()),
	}

	jsonBytes, err := json.Marshal(entry)
	if err != nil {
		return "", fmt.Errorf("序列化响应失败: %v", err)
	}

	return string(jsonBytes), nil
}

// handleFileChmod 处理权限修改请求
func (a *AgentClient) handleFileChmod(data string) (string, error) {
	var req FileChmodRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.Path == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	absPath, err := resolvePath(req.Path)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %v", err)
	}

	if runtime.GOOS == "windows" {
		return "", fmt.Errorf("Windows 系统不支持 chmod 操作")
	}

	if err := os.Chmod(absPath, os.FileMode(req.Mode)); err != nil {
		return "", fmt.Errorf("修改权限失败: %v", err)
	}

	return "权限修改成功", nil
}

// handleFileDownloadChunk 处理文件分块下载请求
func (a *AgentClient) handleFileDownloadChunk(data string) (string, error) {
	var req FileDownloadChunkRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.Path == "" {
		return "", fmt.Errorf("文件路径不能为空")
	}

	if req.Size <= 0 || req.Size > 2*1024*1024 { // 限制单块最大 2MB
		req.Size = 1024 * 1024 // 默认1MB
	}

	absPath, err := resolvePath(req.Path)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %v", err)
	}

	file, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %v", err)
	}
	defer file.Close()

	if _, err := file.Seek(req.Offset, 0); err != nil {
		return "", fmt.Errorf("定位文件失败: %v", err)
	}

	buffer := make([]byte, req.Size)
	n, err := file.Read(buffer)
	if err != nil && err.Error() != "EOF" {
		return "", fmt.Errorf("读取文件失败: %v", err)
	}

	return base64.StdEncoding.EncodeToString(buffer[:n]), nil
}
