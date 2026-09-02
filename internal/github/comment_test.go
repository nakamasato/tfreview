package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nakamasato/tfreview/internal/render"
	"github.com/stretchr/testify/require"
)

type call struct{ method, path, body string }

func server(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, calls *[]call)) (*Client, *[]call) {
	t.Helper()
	calls := &[]call{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*calls = append(*calls, call{r.Method, r.URL.Path + "?" + r.URL.RawQuery, string(b)})
		require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		handler(w, r, calls)
	}))
	t.Cleanup(srv.Close)
	c := New("o/r", "tok")
	c.BaseURL = srv.URL
	return c, calls
}

func TestUpsertCommentCreates(t *testing.T) {
	c, calls := server(t, func(w http.ResponseWriter, r *http.Request, _ *[]call) {
		switch {
		case r.Method == "GET":
			_, _ = w.Write([]byte(`[{"id":1,"body":"unrelated"}]`))
		case r.Method == "POST":
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":2}`))
		}
	})
	require.NoError(t, c.UpsertComment(context.Background(), 7, render.Begin+"\nnew\n"+render.End))
	require.Equal(t, "POST", (*calls)[1].method)
	require.Equal(t, "/repos/o/r/issues/7/comments?", (*calls)[1].path)
	require.Contains(t, (*calls)[1].body, "new")
}

func TestUpsertCommentPatchesExisting(t *testing.T) {
	existing := "keep me\n" + render.Begin + "\nold\n" + render.End
	c, calls := server(t, func(w http.ResponseWriter, r *http.Request, _ *[]call) {
		switch r.Method {
		case "GET":
			b, _ := json.Marshal([]map[string]any{{"id": 5, "body": existing}})
			_, _ = w.Write(b)
		case "PATCH":
			_, _ = w.Write([]byte(`{"id":5}`))
		}
	})
	require.NoError(t, c.UpsertComment(context.Background(), 7, render.Begin+"\nnew\n"+render.End))
	last := (*calls)[len(*calls)-1]
	require.Equal(t, "PATCH", last.method)
	require.Equal(t, "/repos/o/r/issues/comments/5?", last.path)
	require.Contains(t, last.body, "keep me")
	require.Contains(t, last.body, "new")
	require.NotContains(t, last.body, "old")
}

func TestSetLabelReplacesAndCreates(t *testing.T) {
	created := false
	c, calls := server(t, func(w http.ResponseWriter, r *http.Request, _ *[]call) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/labels"):
			_, _ = w.Write([]byte(`[{"name":"tfreview:none"},{"name":"bug"}]`))
		case r.Method == "DELETE":
			w.WriteHeader(200)
		case r.Method == "POST" && r.URL.Path == "/repos/o/r/issues/7/labels":
			if !created {
				w.WriteHeader(404)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case r.Method == "POST" && r.URL.Path == "/repos/o/r/labels":
			created = true
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
		}
	})
	require.NoError(t, c.SetLabel(context.Background(), 7, "tfreview:high"))
	var methods []string
	for _, cl := range *calls {
		methods = append(methods, cl.method+" "+cl.path)
	}
	require.Contains(t, methods, "DELETE /repos/o/r/issues/7/labels/tfreview:none?")
	require.NotContains(t, strings.Join(methods, "\n"), "labels/bug")
	require.Contains(t, methods, "POST /repos/o/r/labels?")
	require.Contains(t, (*calls)[len(*calls)-2].body, "D93F0B")
}

func TestSetLabelForbidden(t *testing.T) {
	c, _ := server(t, func(w http.ResponseWriter, r *http.Request, _ *[]call) {
		switch {
		case r.Method == "GET":
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/repos/o/r/issues/7/labels":
			w.WriteHeader(404)
		default:
			w.WriteHeader(403)
		}
	})
	err := c.SetLabel(context.Background(), 7, "tfreview:high")
	require.ErrorIs(t, err, ErrLabelForbidden)
}
