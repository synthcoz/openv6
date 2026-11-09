package panel

import (
	"fmt"
	"net/http"
	"strings"

	"openv6/internal/store"
)

// ---- accounts: admins and customers (reseller mode) ----

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	us, err := s.Store.ListUsers()
	if err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, us)
}

func validUsername(u string) bool {
	if len(u) < 2 || len(u) > 40 {
		return false
	}
	for _, c := range u {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-@", c)) {
			return false
		}
	}
	return true
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if s.needPro(w, "Multiple accounts") {
		return
	}
	var b struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	b.Username = strings.TrimSpace(b.Username)
	if b.Role != "admin" && b.Role != "customer" {
		fail(w, 400, "role must be admin or customer")
		return
	}
	if !validUsername(b.Username) || len(b.Password) < 8 {
		fail(w, 400, "username: 2-40 letters, digits or . _ - @; password: at least 8 characters")
		return
	}
	if err := s.Store.CreateUser(b.Username, b.Password, b.Role); err != nil {
		fail(w, 400, "username already exists")
		return
	}
	s.Store.AddEvent(0, "info", fmt.Sprintf("%s account %q created", b.Role, b.Username))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.Store.GetUser(pathID(r))
	if err != nil {
		fail(w, 404, "account not found")
		return
	}
	var b struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &b); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if len(b.Password) < 8 {
		fail(w, 400, "password must be at least 8 characters")
		return
	}
	if err := s.Store.SetPassword(u.Username, b.Password); err != nil {
		logErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.Store.GetUser(pathID(r))
	if err != nil {
		fail(w, 404, "account not found")
		return
	}
	if me := currentUser(r); me != nil && me.ID == u.ID {
		fail(w, 400, "you cannot delete your own account")
		return
	}
	if u.Role == "admin" {
		us, _ := s.Store.ListUsers()
		admins := 0
		for _, x := range us {
			if x.Role == "admin" {
				admins++
			}
		}
		if admins <= 1 {
			fail(w, 400, "the last admin account cannot be deleted")
			return
		}
	}
	if err := s.Store.DeleteUser(u.ID); err != nil {
		logErr(w, err)
		return
	}
	s.Store.AddEvent(0, "info", fmt.Sprintf("account %q deleted", u.Username))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

var _ = store.ErrNotFound
