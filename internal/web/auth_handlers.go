package web

import (
	"errors"
	"net"
	"net/http"

	"github.com/atotmakov/home_net_explorer/internal/auth"
)

func (s *Server) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "setup.html", page{Title: "Set up"})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	pw, confirm := r.PostFormValue("password"), r.PostFormValue("confirm")
	if pw != confirm {
		s.render(w, r, http.StatusBadRequest, "setup.html", page{Title: "Set up", Error: "The passwords don't match."})
		return
	}
	switch err := s.opts.Owner.SetPassword(r.Context(), pw); {
	case errors.Is(err, auth.ErrPasswordTooShort):
		s.render(w, r, http.StatusBadRequest, "setup.html", page{Title: "Set up", Error: "Use at least 10 characters."})
		return
	case errors.Is(err, auth.ErrPasswordAlreadySet):
		http.NotFound(w, r)
		return
	case err != nil:
		s.serverError(w, r, err)
		return
	}
	s.startSession(w, r)
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if s.loggedIn(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, http.StatusOK, "login.html", page{Title: "Log in"})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.Allow(clientIP(r)) {
		s.render(w, r, http.StatusTooManyRequests, "login.html",
			page{Title: "Log in", Error: "Too many attempts. Wait a minute and try again."})
		return
	}
	ok, err := s.opts.Owner.Verify(r.Context(), r.PostFormValue("password"))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !ok {
		s.render(w, r, http.StatusUnauthorized, "login.html", page{Title: "Log in", Error: "Wrong password."})
		return
	}
	s.startSession(w, r)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		if err := s.opts.Owner.DeleteSession(r.Context(), c.Value); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	token, err := s.opts.Owner.NewSession(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "home.html", page{Title: "Home", Nav: true, Active: "home"})
}
