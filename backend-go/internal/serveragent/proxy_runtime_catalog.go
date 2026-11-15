package serveragent

// Managed proxy runtime releases are deliberately pinned. Updating them is a
// reviewed application release, never a runtime lookup of GitHub's `latest`.
type proxyRuntimeRelease struct {
	Runtime     string
	Version     string
	AMD64URL    string
	AMD64SHA256 string
	ARM64URL    string
	ARM64SHA256 string
	AssetFormat string
}

var managedProxyRuntimeCatalog = map[string]proxyRuntimeRelease{
	"sing-box": {
		// Version 是「上游版本 + 本仓库构建序号」。am2 = 用 Go 1.27.3 重新构建
		// 同一份审计过的 sing-box 1.13.14 源码（am1 用的是 Go 1.26.4，受
		// CVE-2026-111943 / GO-2026-6809 影响）。构建序号写进 Version，
		// 各主机才会把已安装的 am1 判定为需要升级。
		Runtime: "sing-box", Version: "1.13.14-am3",
		AMD64URL: "https://github.com/iwvw/API-Monitor/releases/download/managed-runtime-v1.13.14-am3/sing-box-1.13.14-linux-amd64", AMD64SHA256: "b32a35e9efb9e452b37612523052810a0350f0bbee395a627c746babef0cff66",
		ARM64URL: "https://github.com/iwvw/API-Monitor/releases/download/managed-runtime-v1.13.14-am3/sing-box-1.13.14-linux-arm64", ARM64SHA256: "bf6bc4472b6c61a8e6b33d59896bab7e7f8be072e5f351de6d0d979ea795a6fd",
		AssetFormat: "binary",
	},
}

func managedProxyRuntime(runtime string) (proxyRuntimeRelease, bool) {
	// The internal data plane is intentionally uniform. sing-box supports both
	// VLESS REALITY/XTLS Vision and Hysteria2, avoiding two independent core
	// installation and security-update paths.
	if runtime == "xray" || runtime == "singbox" {
		runtime = "sing-box"
	}
	release, ok := managedProxyRuntimeCatalog[runtime]
	return release, ok
}
