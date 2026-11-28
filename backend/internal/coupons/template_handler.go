package coupons

import (
	"net/http"
	"strings"
)

func (h Handler) templates(w http.ResponseWriter, r *http.Request, owner string) {
	if r.URL.RawQuery != "" {
		respond(w, 200, nil, ErrInvalid)
		return
	}
	var result any
	var err error
	code := 200
	switch {
	case r.URL.Path == "/api/coupons/templates/search":
		if !method(w, r, "POST") {
			return
		}
		var in TemplateSearch
		if !decode(w, r, &in) || !h.lookup(w, r, owner) {
			return
		}
		result, err = h.Store.SearchTemplates(r.Context(), owner, in)
	case r.URL.Path == "/api/coupons/templates":
		if !method(w, r, "POST") {
			return
		}
		one := 1
		in := TemplateInput{PerClientLimit: &one}
		if !decode(w, r, &in) {
			return
		}
		var replay bool
		result, replay, err = h.Store.SaveTemplate(r.Context(), owner, "", r.Header.Get("Idempotency-Key"), in)
		if !replay {
			code = 201
		}
	default:
		id := strings.TrimPrefix(r.URL.Path, "/api/coupons/templates/")
		if !validUUID(id) {
			respond(w, 200, nil, ErrNotFound)
			return
		}
		switch r.Method {
		case "GET":
			if !h.lookup(w, r, owner) {
				return
			}
			result, err = h.Store.GetTemplate(r.Context(), owner, id)
		case "PUT":
			one := 1
			in := TemplateInput{PerClientLimit: &one}
			if !decode(w, r, &in) {
				return
			}
			result, _, err = h.Store.SaveTemplate(r.Context(), owner, id, r.Header.Get("Idempotency-Key"), in)
		default:
			w.Header().Set("Allow", "GET, PUT")
			reply(w, 405, map[string]string{"status": "method_not_allowed"})
			return
		}
	}
	respond(w, code, result, err)
}
