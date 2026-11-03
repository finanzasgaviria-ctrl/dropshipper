package api

import (
	"net/http"

	"github.com/michiTrader/dropshipper/motor/internal/settings"
)

// Routes of the ngrok tunnel (public address for the webhooks).
func (s *Server) ngrokRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/ngrok/estado", s.ngrokStatus)
	api.HandleFunc("PUT /api/ajustes/ngrok", s.putNgrok)
	api.HandleFunc("DELETE /api/ajustes/ngrok", s.delNgrok)
	api.HandleFunc("POST /api/ngrok/reintentar", s.ngrokRetry)
}

func (s *Server) ngrokStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Hub.Tunnel.Info())
}

type ngrokIn struct {
	Token  string  `json:"token"`
	Domain *string `json:"dominio"`
	Active *bool   `json:"activo"`
}

// putNgrok saves the token and domain and switches the tunnel on or off. An
// empty token means "keep the saved one"; pasting a token turns the tunnel on
// unless "activo" says otherwise.
func (s *Server) putNgrok(w http.ResponseWriter, r *http.Request) {
	var in ngrokIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if s.update(w, r, func(st *settings.Settings) error {
		n := st.Ngrok
		if t := settings.CleanNgrokToken(in.Token); t != "" {
			n.Token = t
			if in.Active == nil {
				n.Enabled = true
			}
		}
		if in.Domain != nil {
			n.Domain = *in.Domain
		}
		if in.Active != nil {
			n.Enabled = *in.Active
		}
		st.Ngrok = n
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true, "tunel": s.Hub.Tunnel.Info()})
	}
}

func (s *Server) delNgrok(w http.ResponseWriter, r *http.Request) {
	if s.update(w, r, func(st *settings.Settings) error {
		st.Ngrok = settings.Ngrok{}
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

// ngrokRetry reconnects, e.g. after closing the ngrok that held the address.
func (s *Server) ngrokRetry(w http.ResponseWriter, r *http.Request) {
	s.Hub.Tunnel.Restart()
	writeJSON(w, 200, map[string]any{"ok": true, "tunel": s.Hub.Tunnel.Info()})
}
