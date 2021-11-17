package main

import (
	"flag"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/pelletier/go-toml/v2"
)

func main() {
	cfgpath := flag.String("config", "govanity.toml", "path to config")
	flag.Parse()
	cfg, err := readcfg(*cfgpath)
	if err != nil {
		log.Fatalln(err)
	}
	if cfg.SocketPath == "" {
		log.Fatalln("SocketPath is unset")
	}
	var sockperm fs.FileMode
	if cfg.SocketPerm != "" {
		n, err := strconv.ParseUint(cfg.SocketPerm, 8, 32)
		if err != nil {
			log.Fatalf("invalid SocketPerm value %s: %s\n", cfg.SocketPerm, err)
		}
		sockperm = fs.FileMode(n)
	}
	l, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		log.Fatalln(err)
	}
	if cfg.SocketPerm != "" {
		if err := os.Chmod(cfg.SocketPath, sockperm); err != nil {
			l.Close()
			log.Fatalln(err)
		}
	}
	sigs := make(chan os.Signal, 1)
	httperr := make(chan error)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		httperr <- http.Serve(l, cfg)
	}()
	select {
	case err := <-httperr:
		l.Close()
		log.Fatalln(err)
	case sig := <-sigs:
		l.Close()
		log.Println("signal received:", sig)
	}
}

type Config struct {
	Base       string
	Modules    map[string]string
	Fallback   string
	SocketPath string
	SocketPerm string
}

func readcfg(fname string) (*Config, error) {
	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var config Config
	return &config, toml.NewDecoder(f).Decode(&config)
}

var template = `<!DOCTYPE html>
<html>
<head>
<meta http-equiv="Content-Type" content="text/html; charset=utf-8"/>
<meta name="go-import" content="MOD SRC">
<meta http-equiv="refresh" content="0; url=https://pkg.go.dev/PKG">
</head>
<body>
`

var rootTemplate = `<!DOCTYPE html>
<html>
<head>
<meta http-equiv="Content-Type" content="text/html; charset=utf-8"/>
<meta http-equiv="refresh" content="0; url=https://pkg.go.dev/?q=HOST">
</head>
<body>
`

func (h *Config) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		whtml(w, strings.Replace(rootTemplate, "HOST", h.Base, 1))
		return
	}
	var mod string
	var src string
	for modname, modsrc := range h.Modules {
		if strings.HasPrefix(r.URL.Path[1:], modname) {
			mod = modname
			src = modsrc
			break
		}
	}
	if mod == "" {
		mod = strings.SplitN(r.URL.Path, "/", 3)[1]
		src = strings.ReplaceAll(h.Fallback, "%", mod)
	}
	rep := strings.NewReplacer(
		"MOD", h.Base+"/"+mod,
		"PKG", h.Base+r.URL.Path,
		"SRC", src,
	)
	whtml(w, rep.Replace(template))
}

func whtml(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/html")
	io.WriteString(w, s)
}
