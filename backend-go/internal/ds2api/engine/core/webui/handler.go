package webui

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// welcomeHTML 是 "/" 落地页的 Go fallback（webui 未构建或直接访问根路径时展示）。
// 其内联 i18n 字典（key: badge/subtitle/admin/apiStatus/f1t~f4t/f1d~f4d/footer/
// themeLabel/langLabel）与 webui/src/locales/{zh,en}.json 的 landing.* 键对应——
// React 落地页（webui/src/components/LandingPage.jsx）使用同一落地页的 webui 版本。
// 维护时请同步更新两边，避免两边文案漂移。
const welcomeHTML = `<!DOCTYPE html>
<html lang="zh-CN" data-theme="dark">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>DS2API</title>
<script>
(function(){
  try {
    var root = document.documentElement;
    var t = localStorage.getItem('ds2api_theme');
    if (t === 'light' || t === 'dark') root.dataset.theme = t;
    var l = localStorage.getItem('ds2api_lang');
    if (l === 'zh' || l === 'en') root.lang = l === 'zh' ? 'zh-CN' : 'en';
  } catch(e){}
})();
</script>
<style>
:root{
  --bg:hsl(222,26%,9%); --fg:hsl(210,20%,96%);
  --card:hsl(222,24%,12%); --card-fg:hsl(210,20%,96%);
  --muted:hsl(222,15%,20%); --muted-fg:hsl(217,12%,62%);
  --primary:hsl(45,93%,47%); --primary-fg:hsl(240,10%,3.9%);
  --border:hsl(222,14%,22%);
  --radius:.625rem;
}
[data-theme="light"]{
  --bg:hsl(210,25%,97%); --fg:hsl(222,30%,13%);
  --card:hsl(0,0%,100%); --card-fg:hsl(222,30%,13%);
  --muted:hsl(213,22%,93%); --muted-fg:hsl(220,10%,42%);
  --primary:hsl(45,93%,50%); --primary-fg:hsl(240,10%,3.9%);
  --border:hsl(216,18%,88%);
}
*{box-sizing:border-box;border-color:var(--border)}
html{color-scheme:dark}
html[data-theme="light"]{color-scheme:light}
body{
  margin:0;min-height:100vh;display:flex;flex-direction:column;
  background:var(--bg);color:var(--fg);
  font-family:Inter,system-ui,-apple-system,sans-serif;
  -webkit-font-smoothing:antialiased;
}
.app-backdrop{
  background:
    radial-gradient(60rem 30rem at 85% -10%, hsl(45,93%,47%/.10), transparent 60%),
    radial-gradient(50rem 26rem at -10% 110%, hsl(222,40%,30%/.35), transparent 60%),
    var(--bg);
}
[data-theme="light"] .app-backdrop{
  background:
    radial-gradient(60rem 30rem at 85% -10%, hsl(45,93%,50%/.08), transparent 60%),
    radial-gradient(50rem 26rem at -10% 110%, hsl(213,60%,80%/.5), transparent 60%),
    var(--bg);
}
.topbar{
  position:fixed;top:1rem;right:1rem;z-index:20;
  display:flex;gap:.5rem;
}
.btn{
  display:inline-flex;align-items:center;justify-content:center;gap:.375rem;
  border-radius:var(--radius);padding:.5rem 1rem;font-size:.875rem;font-weight:500;
  line-height:1.25rem;transition:background-color .15s,color .15s,border-color .15s,
    box-shadow .15s,transform .05s;
  border:1px solid var(--border);
  background:var(--card);color:var(--muted-fg);
  backdrop-filter:blur(8px);cursor:pointer;
}
.btn:hover{color:var(--fg);border-color:var(--primary)}
.btn:focus-visible{outline:none;box-shadow:0 0 0 2px var(--bg),0 0 0 4px var(--primary)}
.btn-primary{
  background:var(--primary);color:var(--primary-fg);border-color:transparent;
  box-shadow:0 1px 2px 0 rgb(0 0 0/.08);
}
.btn-primary:hover{filter:brightness(1.06);color:var(--primary-fg)}
.btn-primary:active{transform:translateY(1px)}
.btn-sm{padding:.375rem .75rem;font-size:.75rem}
main{
  flex:1;display:flex;flex-direction:column;align-items:center;justify-content:center;
  padding:6rem 1.5rem 3rem;max-width:56rem;margin:0 auto;text-align:center;
}
.badge{
  display:inline-flex;align-items:center;gap:.375rem;
  border-radius:9999px;border:1px solid var(--border);background:var(--card);
  padding:.375rem .875rem;font-size:.75rem;font-weight:500;color:var(--muted-fg);
}
.badge svg{color:var(--primary)}
h1{
  margin:1.5rem 0 0;font-size:clamp(2.5rem,6vw,3.75rem);font-weight:700;letter-spacing:-.02em;
}
h1 span{color:var(--primary)}
.subtitle{
  margin-top:1rem;max-width:32rem;font-size:1rem;line-height:1.6;color:var(--muted-fg);
}
.actions{
  margin-top:2.25rem;display:flex;flex-wrap:wrap;gap:.75rem;justify-content:center;
}
.features{
  margin-top:4rem;display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));
  gap:1rem;width:100%;text-align:left;
}
.feature{
  border-radius:calc(var(--radius) + .125rem);border:1px solid var(--border);
  background:var(--card);padding:1.25rem;transition:all .2s;
}
.feature:hover{transform:translateY(-2px);border-color:var(--primary);box-shadow:0 4px 12px rgb(0 0 0/.08)}
.feature-icon{
  display:flex;height:2.25rem;width:2.25rem;align-items:center;justify-content:center;
  border-radius:.5rem;background:color-mix(in srgb,var(--primary) 10%,transparent);color:var(--primary);
  transition:all .2s;
}
.feature:hover .feature-icon{background:var(--primary);color:var(--primary-fg)}
.feature h3{margin:.875rem 0 0;font-size:.875rem;font-weight:600}
.feature p{margin:.375rem 0 0;font-size:.75rem;line-height:1.5;color:var(--muted-fg)}
footer{
  margin-top:4rem;font-size:.75rem;color:var(--muted-fg);opacity:.6;
}
@keyframes fadeInDown{from{opacity:0;transform:translateY(-16px)}to{opacity:1;transform:translateY(0)}}
@keyframes fadeInDownFaded{from{opacity:0;transform:translateY(-16px)}to{opacity:.6;transform:translateY(0)}}
.topbar{animation:fadeInDown .3s ease-out both;animation-delay:.05s}
.badge{animation:fadeInDown .3s ease-out both;animation-delay:.075s}
h1{animation:fadeInDown .3s ease-out both;animation-delay:.15s}
.subtitle{animation:fadeInDown .3s ease-out both;animation-delay:.225s}
.actions{animation:fadeInDown .3s ease-out both;animation-delay:.3s}
.feature{animation:fadeInDown .3s ease-out backwards}
.feature:nth-child(1){animation-delay:.375s}
.feature:nth-child(2){animation-delay:.425s}
.feature:nth-child(3){animation-delay:.475s}
.feature:nth-child(4){animation-delay:.525s}
footer{animation:fadeInDownFaded .3s ease-out both;animation-delay:.575s}
@media (prefers-reduced-motion:reduce){*{animation:none!important}}
</style>
</head>
<body class="app-backdrop">
<div class="topbar">
  <button class="btn btn-sm" id="theme-toggle" title="切换主题 / Toggle theme" aria-label="切换主题 / Toggle theme">
    <svg id="icon-sun" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="display:none"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></svg>
    <svg id="icon-moon" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>
    <span id="theme-label">亮色模式</span>
  </button>
  <button class="btn btn-sm" id="lang-toggle" title="Switch language / 切换语言" aria-label="Switch language / 切换语言">
    <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 8 6 6"/><path d="m4 14 6-6 2-3"/><path d="M2 5h12"/><path d="M7 2h1"/><path d="m22 22-5-10-5 10"/><path d="M14 18h6"/></svg>
    <span id="lang-label">EN</span>
  </button>
</div>

<main>
  <div class="badge">
    <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z"/></svg>
    <span data-i18n="badge">DeepSeek → OpenAI & Claude 兼容网关</span>
  </div>

  <h1>DS2<span>API</span></h1>
  <p class="subtitle" data-i18n="subtitle">将 DeepSeek 模型无缝接入 OpenAI 与 Claude 生态，支持负载均衡、会话管理与工具调用。</p>

  <div class="actions">
    <a href="/admin" class="btn btn-primary" data-i18n="admin">管理面板</a>
    <a href="/v1/models" class="btn" data-i18n="apiStatus">API 状态</a>
    <a href="https://github.com/CJackHwang/ds2api" target="_blank" rel="noreferrer" class="btn">GitHub</a>
  </div>

  <div class="features">
    <div class="feature">
      <div class="feature-icon">
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/></svg>
      </div>
      <h3 data-i18n="f1t">多协议兼容</h3>
      <p data-i18n="f1d">同时支持 OpenAI 与 Claude API 格式，无需修改客户端代码。</p>
    </div>
    <div class="feature">
      <div class="feature-icon">
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 18h8"/><path d="M3 22h18"/><path d="M14 22a7 7 0 1 0 0-14h-1"/><path d="M9 14h2"/><path d="M9 12a2 2 0 0 1-2-2V6h6v4a2 2 0 0 1-2 2Z"/><path d="M12 6V3a1 1 0 0 0-1-1H9a1 1 0 0 0-1 1v3"/></svg>
      </div>
      <h3 data-i18n="f2t">负载均衡</h3>
      <p data-i18n="f2d">多账号池自动分配请求，提升并发与可用性。</p>
    </div>
    <div class="feature">
      <div class="feature-icon">
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/><path d="M20 3v4"/><path d="M22 5h-4"/><path d="M4 17v2"/><path d="M5 18H3"/></svg>
      </div>
      <h3 data-i18n="f3t">工具调用</h3>
      <p data-i18n="f3d">原生支持 Function Calling 与文件引用，释放模型全部能力。</p>
    </div>
    <div class="feature">
      <div class="feature-icon">
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 11h3a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-5Zm0 0a9 9 0 0 1 18 0m0 0v5a2 2 0 0 1-2 2h-1a2 2 0 0 1-2-2v-3a2 2 0 0 1 2-2h3Z"/><path d="M21 16v2a4 4 0 0 1-4 4h-5"/></svg>
      </div>
      <h3 data-i18n="f4t">会话管理</h3>
      <p data-i18n="f4d">可视化聊天历史、会话统计与一键清理。</p>
    </div>
  </div>

  <footer>
    <p data-i18n="footer">&copy; 2026 DS2API Project · 为灵活性与性能而设计</p>
  </footer>
</main>

<script>
(function(){
  var root = document.documentElement;
  var themeKey = 'ds2api_theme';
  var langKey = 'ds2api_lang';
  var i18n = {
    zh: {
      badge:'DeepSeek → OpenAI & Claude 兼容网关',
      subtitle:'将 DeepSeek 模型无缝接入 OpenAI 与 Claude 生态，支持负载均衡、会话管理与工具调用。',
      admin:'管理面板', apiStatus:'API 状态',
      f1t:'多协议兼容', f1d:'同时支持 OpenAI 与 Claude API 格式，无需修改客户端代码。',
      f2t:'负载均衡', f2d:'多账号池自动分配请求，提升并发与可用性。',
      f3t:'工具调用', f3d:'原生支持 Function Calling 与文件引用，释放模型全部能力。',
      f4t:'会话管理', f4d:'可视化聊天历史、会话统计与一键清理。',
      footer:'© 2026 DS2API Project · 为灵活性与性能而设计',
      themeLabel:'亮色模式', langLabel:'EN'
    },
    en: {
      badge:'DeepSeek → OpenAI & Claude compatible gateway',
      subtitle:'Seamlessly connect DeepSeek models to OpenAI & Claude ecosystems with load balancing, session management, and tool calling.',
      admin:'Admin Console', apiStatus:'API Status',
      f1t:'Multi-protocol', f1d:'Supports both OpenAI and Claude API formats without client-side changes.',
      f2t:'Load Balancing', f2d:'Distributes requests across account pools for better concurrency and availability.',
      f3t:'Tool Calling', f3d:'Native support for Function Calling and file references to unlock full model capabilities.',
      f4t:'Session Management', f4d:'Visualize chat history, session stats, and one-click cleanup.',
      footer:'© 2026 DS2API Project · Designed for flexibility & performance',
      themeLabel:'Light mode', langLabel:'中文'
    }
  };

  function getTheme(){
    try { return localStorage.getItem(themeKey) || 'dark'; } catch(e){ return 'dark'; }
  }
  function setTheme(t){
    root.dataset.theme = t;
    try { localStorage.setItem(themeKey, t); } catch(e){}
    updateThemeIcon(t);
  }
  function updateThemeIcon(t){
    var sun = document.getElementById('icon-sun');
    var moon = document.getElementById('icon-moon');
    var label = document.getElementById('theme-label');
    if(t === 'dark'){
      sun.style.display = 'block'; moon.style.display = 'none';
      label.textContent = getLang() === 'zh' ? '亮色模式' : 'Light mode';
    } else {
      sun.style.display = 'none'; moon.style.display = 'block';
      label.textContent = getLang() === 'zh' ? '暗色模式' : 'Dark mode';
    }
  }

  function getLang(){
    try { return localStorage.getItem(langKey) || 'zh'; } catch(e){ return 'zh'; }
  }
  function setLang(l){
    try { localStorage.setItem(langKey, l); } catch(e){}
    applyLang(l);
  }
  function applyLang(l){
    var dict = i18n[l] || i18n.zh;
    document.querySelectorAll('[data-i18n]').forEach(function(el){
      var key = el.getAttribute('data-i18n');
      if(dict[key]) el.textContent = dict[key];
    });
    var langBtn = document.getElementById('lang-label');
    if(langBtn) langBtn.textContent = l === 'zh' ? 'EN' : '中文';
    updateThemeIcon(getTheme());
    root.lang = l === 'zh' ? 'zh-CN' : 'en';
  }

  document.getElementById('theme-toggle').addEventListener('click', function(){
    setTheme(getTheme() === 'dark' ? 'light' : 'dark');
  });
  document.getElementById('lang-toggle').addEventListener('click', function(){
    setLang(getLang() === 'zh' ? 'en' : 'zh');
  });

  setTheme(getTheme());
  applyLang(getLang());
})();
</script>
</body>
</html>`

type Handler struct {
	StaticDir string
}

func NewHandler() *Handler {
	return &Handler{StaticDir: resolveStaticAdminDir(config.StaticAdminDir())}
}

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/", h.index)
	r.Get("/admin", h.admin)
}

func (h *Handler) HandleAdminFallback(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if !strings.HasPrefix(r.URL.Path, "/admin/") {
		return false
	}
	h.admin(w, r)
	return true
}

func (h *Handler) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(welcomeHTML))
}

func (h *Handler) admin(w http.ResponseWriter, r *http.Request) {
	staticDir := resolveStaticAdminDir(h.StaticDir)
	if fi, err := os.Stat(staticDir); err == nil && fi.IsDir() {
		h.serveFromDisk(w, r, staticDir)
		return
	}
	http.Error(w, "WebUI not built. Run `cd webui && npm run build` first.", http.StatusNotFound)
}

// staticContentTypes pins the Content-Type of common WebUI assets so we do not
// rely on mime.TypeByExtension, which on Windows consults the registry and can
// return the wrong type (e.g. application/xml for .css) when third-party
// software has overwritten HKEY_CLASSES_ROOT entries. Browsers strictly enforce
// stylesheet/script MIME types and will refuse to apply a misidentified asset,
// breaking the /admin page on affected machines.
var staticContentTypes = map[string]string{
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".txt":   "text/plain; charset=utf-8",
	".wasm":  "application/wasm",
}

// setStaticContentType pins the response Content-Type by file extension so that
// http.ServeFile does not fall back to mime.TypeByExtension (which on Windows
// reads the registry and may return an incorrect type).
func setStaticContentType(w http.ResponseWriter, fullPath string) {
	ext := strings.ToLower(filepath.Ext(fullPath))
	if ct, ok := staticContentTypes[ext]; ok {
		w.Header().Set("Content-Type", ct)
	}
}

func (h *Handler) serveFromDisk(w http.ResponseWriter, r *http.Request, staticDir string) {
	root := filepath.Clean(staticDir)
	path := strings.TrimPrefix(r.URL.Path, "/admin")
	path = strings.TrimPrefix(path, "/")
	if path != "" && strings.Contains(path, ".") {
		full := filepath.Join(root, filepath.Clean(path))
		if !isPathInsideRoot(full, root) {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(full); err == nil {
			if strings.HasPrefix(path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-store, must-revalidate")
			}
			setStaticContentType(w, full)
			http.ServeFile(w, r, full)
			return
		}
		http.NotFound(w, r)
		return
	}
	index := filepath.Join(root, "index.html")
	if _, err := os.Stat(index); err != nil {
		http.Error(w, "WebUI index not found. Run `cd webui && npm run build` first.", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	setStaticContentType(w, index)
	http.ServeFile(w, r, index)
}

func isPathInsideRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolveStaticAdminDir(configured string) string {
	// 显式配置优先：路径可能尚未存在（auto-build 会创建），无条件信任。
	if strings.TrimSpace(os.Getenv("DS2API_STATIC_ADMIN_DIR")) != "" {
		return configured
	}
	candidates := []string{
		configured,
		filepath.Join("..", "static", "admin"),
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "static", "admin"),
			filepath.Join(exeDir, "..", "static", "admin"),
		)
	}
	if config.IsVercel() {
		candidates = append(candidates, "/var/task/static/admin")
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return configured
}
