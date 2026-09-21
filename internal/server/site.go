package server

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	frontend "github.com/amazonmio/mio-image-hosting/web"
	"gopkg.in/yaml.v3"
)

const (
	defaultSiteName    = "Mio 图床"
	defaultAvatarFile  = "avatar.webp"
	defaultFaviconFile = "favicon.webp"
	maxBrandNameRunes  = 60
	maxBrandFileSize   = 2 << 20
	brandAvatarURL     = "/branding/avatar"
	brandFaviconURL    = "/branding/favicon"
)

var brandMIME = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".ico":  "image/x-icon",
}

const defaultConfigYAML = `# 站点名称，显示在侧栏、登录页和浏览器标签
site_name: Mio 图床

# 更换头像：替换本目录的 avatar.webp
# 更换标签页图标：替换本目录的 favicon.webp
# 改完后重启服务。也可改成其他文件名：
# avatar: avatar.webp
# favicon: favicon.webp
`

type siteFile struct {
	SiteName string `yaml:"site_name"`
	Avatar   string `yaml:"avatar"`
	Favicon  string `yaml:"favicon"`
}

func (a *App) loadSiteConfig() error {
	a.siteName = defaultSiteName
	a.avatarPath, a.faviconPath = "", ""
	a.avatarMIME, a.faviconMIME = "", ""
	if err := a.ensureDefaultBrandFiles(); err != nil {
		return err
	}
	path := filepath.Join(a.dir, "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err = os.WriteFile(path, []byte(defaultConfigYAML), 0600); err != nil {
			return err
		}
		return a.useBrandFiles(defaultAvatarFile, defaultFaviconFile)
	}
	var cfg siteFile
	if err = yaml.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("config.yaml: %w", err)
	}
	name := strings.TrimSpace(cfg.SiteName)
	if name == "" {
		name = defaultSiteName
	}
	if err = validateSiteName(name); err != nil {
		return err
	}
	a.siteName = name
	avatar := strings.TrimSpace(cfg.Avatar)
	if avatar == "" {
		avatar = defaultAvatarFile
	}
	favicon := strings.TrimSpace(cfg.Favicon)
	if favicon == "" {
		favicon = defaultFaviconFile
	}
	return a.useBrandFiles(avatar, favicon)
}

func (a *App) ensureDefaultBrandFiles() error {
	for _, name := range []string{defaultAvatarFile, defaultFaviconFile} {
		path := filepath.Join(a.dir, name)
		_, err := os.Stat(path)
		if err == nil {
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		if err = os.WriteFile(path, frontend.DefaultAvatar, 0600); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) useBrandFiles(avatar, favicon string) error {
	var err error
	a.avatarPath, a.avatarMIME, err = resolveBrandFile(a.dir, avatar, "avatar")
	if err != nil {
		return err
	}
	a.faviconPath, a.faviconMIME, err = resolveBrandFile(a.dir, favicon, "favicon")
	return err
}

func validateSiteName(name string) error {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxBrandNameRunes {
		return errors.New("site_name 须为 1–60 个字符")
	}
	for _, r := range name {
		if r == '\uFFFD' || unicode.IsControl(r) {
			return errors.New("site_name 不能包含控制字符")
		}
	}
	return nil
}

func resolveBrandFile(dataDir, name, field string) (string, string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return "", "", nil
	}
	if strings.Contains(name, "\\") || !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", "", fmt.Errorf("%s 路径无效", field)
	}
	full := filepath.Join(dataDir, filepath.FromSlash(name))
	rel, err := filepath.Rel(dataDir, full)
	if err != nil || !filepath.IsLocal(rel) {
		return "", "", fmt.Errorf("%s 路径无效", field)
	}
	base := strings.ToLower(rel)
	if base == "uploads" || strings.HasPrefix(base, "uploads"+string(os.PathSeparator)) ||
		base == "app.db" || strings.HasPrefix(base, "app.db-") || base == "config.yaml" || base == "setup-key.txt" {
		return "", "", fmt.Errorf("%s 不能使用数据目录中的系统文件", field)
	}
	ext := strings.ToLower(filepath.Ext(full))
	mime, ok := brandMIME[ext]
	if !ok {
		return "", "", fmt.Errorf("%s 仅支持 png、jpg、webp、gif、ico", field)
	}
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("%s 文件不存在: %s", field, name)
		}
		return "", "", err
	}
	if info.IsDir() || info.Size() == 0 || info.Size() > maxBrandFileSize {
		return "", "", fmt.Errorf("%s 文件无效", field)
	}
	return full, mime, nil
}

func (a *App) siteConfig() map[string]any {
	return map[string]any{
		"max_file_size":   maxFileSize,
		"auth_required":   true,
		"public_base_url": a.baseURL,
		"site_name":       a.siteName,
		"avatar_url":      brandAvatarURL,
		"favicon_url":     brandFaviconURL,
	}
}

func (a *App) serveAvatar(w http.ResponseWriter, r *http.Request) {
	a.serveBrand(w, r, a.avatarPath, a.avatarMIME)
}

func (a *App) serveFavicon(w http.ResponseWriter, r *http.Request) {
	a.serveBrand(w, r, a.faviconPath, a.faviconMIME)
}

func (a *App) serveBrand(w http.ResponseWriter, r *http.Request, path, contentType string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if path != "" {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.ServeFile(w, r, path)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, "logo.webp", time.Time{}, bytes.NewReader(frontend.DefaultAvatar))
}

func injectSite(page []byte, siteName string) []byte {
	s := string(page)
	escaped := html.EscapeString(siteName)
	if i := strings.Index(s, "<title>"); i >= 0 {
		if j := strings.Index(s[i:], "</title>"); j >= 0 {
			s = s[:i] + "<title>" + escaped + "</title>" + s[i+j+len("</title>"):]
		}
	}
	s = strings.Replace(s, `<link rel="icon" type="image/webp" href="/logo.webp"/>`, `<link rel="icon" href="`+brandFaviconURL+`"/>`, 1)
	return []byte(s)
}

func (a *App) serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "Frontend is not built. Run: cd web && npm ci && npm run build; then restart Go.", http.StatusServiceUnavailable)
		return
	}
	body := injectSite(raw, a.siteName)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		return
	}
	w.Write(body)
}
