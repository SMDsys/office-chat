package main

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFiles embed.FS

var version = "1.2.0"

type Config struct {
	Port    int    `json:"port"`
	Host    string `json:"host"`
	DataDir string `json:"dataDir"`
	TLS     bool   `json:"tls"`
	TLSCert string `json:"tlsCert,omitempty"`
	TLSKey  string `json:"tlsKey,omitempty"`
}

type runtime struct {
	cfg    Config
	store  *Store
	hub    *Hub
	srv    *http.Server
	ln     net.Listener
	logger *log.Logger
	scheme string
}

func main() {
	install := flag.Bool("install", false, "установить службу Windows")
	uninstall := flag.Bool("uninstall", false, "удалить службу Windows")
	service := flag.Bool("service", false, "запуск в режиме службы Windows")
	port := flag.Int("port", 0, "порт (переопределяет config.json)")
	dataDir := flag.String("data", "", "каталог данных (переопределяет config.json)")
	showVersion := flag.Bool("version", false, "показать версию")
	passwd := flag.String("passwd", "", "сменить пароль: -passwd логин:новыйпароль")
	tlsOn := flag.Bool("tls", false, "включить HTTPS с самоподписанным сертификатом")
	tlsCert := flag.String("tls-cert", "", "путь к сертификату (PEM), вместе с -tls-key")
	tlsKey := flag.String("tls-key", "", "путь к ключу (PEM), вместе с -tls-cert")
	flag.Parse()

	if *showVersion {
		fmt.Println("Офис-чат", version)
		return
	}

	exeDir := executableDir()

	if *install {
		if err := installService(exeDir); err != nil {
			fmt.Fprintln(os.Stderr, "Не удалось установить службу:", err)
			os.Exit(1)
		}
		fmt.Println("Служба «Офис-чат» установлена и запущена.")
		return
	}
	if *uninstall {
		if err := uninstallService(); err != nil {
			fmt.Fprintln(os.Stderr, "Не удалось удалить службу:", err)
			os.Exit(1)
		}
		fmt.Println("Служба «Офис-чат» остановлена и удалена.")
		return
	}

	cfg, err := loadConfig(exeDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Конфигурация:", err)
		os.Exit(1)
	}
	if *port > 0 {
		cfg.Port = *port
	}
	if *dataDir != "" {
		if !filepath.IsAbs(*dataDir) {
			*dataDir = filepath.Join(exeDir, *dataDir)
		}
		cfg.DataDir = *dataDir
	}

	if *tlsOn {
		cfg.TLS = true
	}
	if *tlsCert != "" {
		cfg.TLSCert = *tlsCert
	}
	if *tlsKey != "" {
		cfg.TLSKey = *tlsKey
	}
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		cfg.TLS = true
	}

	if *passwd != "" {
		login, password, ok := strings.Cut(*passwd, ":")
		login = strings.TrimSpace(login)
		if !ok || login == "" {
			fmt.Fprintln(os.Stderr, "Формат: OfficeChat.exe -passwd логин:новыйпароль")
			os.Exit(1)
		}
		if len([]rune(password)) < 6 {
			fmt.Fprintln(os.Stderr, "Пароль должен быть не короче 6 символов.")
			os.Exit(1)
		}
		if err := resetPassword(cfg, login, password); err != nil {
			fmt.Fprintln(os.Stderr, "Не удалось сменить пароль:", err)
			os.Exit(1)
		}
		fmt.Printf("Пароль пользователя «%s» изменён.\n", login)
		fmt.Println("Если служба была остановлена, запустите её: net start OfficeChat")
		return
	}

	logger, closeLog, err := newLogger(cfg.DataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Не удалось открыть журнал:", err)
		os.Exit(1)
	}
	defer closeLog()

	if *service || isWindowsService() {
		if err := runAsService(cfg, logger); err != nil {
			logger.Printf("служба остановлена с ошибкой: %v", err)
			os.Exit(1)
		}
		return
	}

	rt, err := startServer(cfg, logger)
	if err != nil {
		logger.Printf("запуск: %v", err)
		fmt.Fprintln(os.Stderr, "Ошибка запуска:", err)
		os.Exit(1)
	}
	printBanner(cfg, logger)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	logger.Println("остановка…")
	rt.Stop()
	logger.Println("остановлено")
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func loadConfig(exeDir string) (Config, error) {
	cfg := Config{Port: 8080, Host: "0.0.0.0", DataDir: "data"}
	path := filepath.Join(exeDir, "config.json")
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("config.json: %w", err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if b, err := json.MarshalIndent(cfg, "", "  "); err == nil {
			_ = os.WriteFile(path, b, 0o644)
		}
	} else {
		return cfg, err
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	if !filepath.IsAbs(cfg.DataDir) {
		cfg.DataDir = filepath.Join(exeDir, cfg.DataDir)
	}
	return cfg, nil
}

func newLogger(dir string) (*log.Logger, func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "server.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	logger := log.New(io.MultiWriter(os.Stdout, f), "", log.Ldate|log.Ltime)
	return logger, func() { _ = f.Close() }, nil
}

func startServer(cfg Config, logger *log.Logger) (*runtime, error) {
	store, err := NewStore(cfg.DataDir, logger)
	if err != nil {
		return nil, fmt.Errorf("хранилище: %w", err)
	}
	hub := NewHub(store, logger)
	app := &App{
		store:   store,
		hub:     hub,
		log:     logger,
		limiter: newLimiter(),
		version: version,
		started: time.Now(),
	}
	handler := app.routes()

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("порт %d занят или недоступен: %w", cfg.Port, err)
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	scheme := "http"
	if cfg.TLS {
		cert, certPath, err := loadOrCreateTLSCert(cfg.DataDir, cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			store.Close()
			return nil, fmt.Errorf("TLS: %w", err)
		}
		srv.TLSConfig = tlsConfig(cert)
		ln = tls.NewListener(ln, srv.TLSConfig)
		app.secure = true
		scheme = "https"
		logger.Println(tlsHint(certPath))
	}
	rt := &runtime{cfg: cfg, store: store, hub: hub, srv: srv, ln: ln, logger: logger, scheme: scheme}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("http: %v", err)
		}
	}()
	logger.Printf("Офис-чат %s запущен на %s://%s, данные: %s", version, scheme, addr, cfg.DataDir)
	store.maybeBackup()
	return rt, nil
}

func (rt *runtime) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = rt.srv.Shutdown(ctx)
	rt.store.Close()
}

func printBanner(cfg Config, logger *log.Logger) {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	fmt.Println()
	fmt.Println("  Офис-чат", version)
	fmt.Println("  ─────────────────────────────────────────")
	fmt.Printf("  На этом компьютере:  %s://localhost:%d\n", scheme, cfg.Port)
	for _, ip := range localIPs() {
		fmt.Printf("  В локальной сети:    %s://%s:%d\n", scheme, ip, cfg.Port)
	}
	if cfg.TLS {
		fmt.Println("  HTTPS включён, сертификат самоподписанный:")
		fmt.Println("  браузер попросит подтвердить исключение один раз.")
	}
	fmt.Printf("  Данные:              %s\n", cfg.DataDir)
	fmt.Println("  Остановить: Ctrl+C")
	fmt.Println()
	logger.Printf("локальные адреса: %v", localIPs())
}

func localIPs() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}

// ---------- Маршруты ----------

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "version": a.version, "uptime": time.Since(a.started).String()})
	})
	mux.HandleFunc("GET /api/state", a.public(a.handleState))
	mux.HandleFunc("POST /api/register", a.public(a.handleRegister))
	mux.HandleFunc("POST /api/login", a.public(a.handleLogin))

	mux.HandleFunc("GET /api/ws", a.auth(func(w http.ResponseWriter, r *http.Request, u *User) {
		a.hub.ServeWS(w, r, u.ID)
	}))
	mux.HandleFunc("POST /api/logout", a.auth(a.handleLogout))
	mux.HandleFunc("GET /api/me", a.auth(a.handleMe))
	mux.HandleFunc("PATCH /api/me", a.auth(a.handleUpdateMe))
	mux.HandleFunc("POST /api/me/password", a.auth(a.handleChangePassword))
	mux.HandleFunc("GET /api/bootstrap", a.auth(a.handleBootstrap))

	mux.HandleFunc("GET /api/messages", a.auth(a.handleMessages))
	mux.HandleFunc("POST /api/messages", a.auth(a.handleSendMessage))
	mux.HandleFunc("PATCH /api/messages/{id}", a.auth(a.handleEditMessage))
	mux.HandleFunc("DELETE /api/messages/{id}", a.auth(a.handleDeleteMessage))
	mux.HandleFunc("POST /api/messages/{id}/react", a.auth(a.handleReact))
	mux.HandleFunc("POST /api/messages/{id}/pin", a.auth(a.handlePin))

	mux.HandleFunc("POST /api/chats", a.auth(a.handleCreateChannel))
	mux.HandleFunc("PATCH /api/chats/{id}", a.auth(a.handleUpdateChannel))
	mux.HandleFunc("DELETE /api/chats/{id}", a.auth(a.handleDeleteChannel))
	mux.HandleFunc("POST /api/dm", a.auth(a.handleCreateDM))
	mux.HandleFunc("POST /api/read", a.auth(a.handleRead))
	mux.HandleFunc("POST /api/read/all", a.auth(a.handleReadAll))
	mux.HandleFunc("POST /api/typing", a.auth(a.handleTyping))
	mux.HandleFunc("GET /api/search", a.auth(a.handleSearch))
	mux.HandleFunc("GET /api/pins", a.auth(a.handlePins))
	mux.HandleFunc("POST /api/upload", a.auth(a.handleUpload))
	mux.HandleFunc("GET /files/{id}", a.handleFile)

	mux.HandleFunc("POST /api/admin/users/{id}/reset", a.auth(a.handleAdminReset))
	mux.HandleFunc("POST /api/admin/users/{id}/block", a.auth(a.handleAdminBlock))

	mux.Handle("/", staticHandler())
	return a.middleware(mux)
}

func (a *App) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				a.log.Printf("паника при запросе %s %s: %v", r.Method, r.URL.Path, rec)
				writeErr(w, 500, "внутренняя ошибка сервера")
			}
			if d := time.Since(start); d > 2*time.Second {
				a.log.Printf("медленный запрос %s %s: %s", r.Method, r.URL.Path, d)
			}
		}()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob:; media-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; script-src 'self'; "+
				"connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func staticHandler() http.Handler {
	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			// неизвестный путь — отдаём интерфейс (одностраничное приложение)
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			p = "index.html"
		}
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})
}
