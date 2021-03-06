package sockjs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_htmlFileNoCallback(t *testing.T) {
	h := newTestHandler()
	rw := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/server/session/htmlfile", nil)
	h.htmlFile(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("Unexpected response code, got '%d', expected '%d'", rw.Code, http.StatusBadRequest)
	}
	expectedContentType := "text/plain; charset=utf-8"
	if rw.Header().Get("content-type") != expectedContentType {
		t.Errorf("Unexpected content type, got '%s', expected '%s'", rw.Header().Get("content-type"), expectedContentType)
	}
}

func TestHandler_htmlFile(t *testing.T) {
	h := newTestHandler()
	rw := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/server/session/htmlfile?c=testCallback", nil)
	h.htmlFile(rw, req)
	if rw.Code != http.StatusOK {
		t.Errorf("Unexpected response code, got '%d', expected '%d'", rw.Code, http.StatusOK)
	}
	expectedContentType := "text/html; charset=UTF-8"
	if rw.Header().Get("content-type") != expectedContentType {
		t.Errorf("Unexpected content-type, got '%s', expected '%s'", rw.Header().Get("content-type"), expectedContentType)
	}
	if rw.Body.String() != expectedIFrame {
		t.Errorf("Unexpected response body, got '%s', expected '%s'", rw.Body, expectedIFrame)
	}
	sess, _ := h.sessionByRequest(req)
	if rt := sess.ReceiverType(); rt != ReceiverTypeHtmlFile {
		t.Errorf("Unexpected recevier type, got '%v', extected '%v'", rt, ReceiverTypeHtmlFile)
	}
}

func TestHandler_cannotIntoXSS(t *testing.T) {
	h := newTestHandler()
	rw := httptest.NewRecorder()
	// test simple injection
	req, _ := http.NewRequest("GET", "/server/session/htmlfile?c=fake%3Balert(1337)", nil)
	h.htmlFile(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("Unexpected response code, got '%d', expected '%d'", rw.Code, http.StatusBadRequest)
	}

	h = newTestHandler()
	rw = httptest.NewRecorder()
	// test simple injection
	req, _ = http.NewRequest("GET", "/server/session/htmlfile?c=fake%2Dalert", nil)
	h.htmlFile(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("Unexpected response code, got '%d', expected '%d'", rw.Code, http.StatusBadRequest)
	}
}

func init() {
	expectedIFrame += strings.Repeat(" ", 1024-len(expectedIFrame)+len("testCallack")+13)
	expectedIFrame += "\r\n\r\n"
	expectedIFrame += "<script>\np(\"o\");\n</script>\r\n"
}

var expectedIFrame = `<!doctype html>
<html><head>
  <meta http-equiv="X-UA-Compatible" content="IE=edge" />
  <meta http-equiv="Content-Type" content="text/html; charset=UTF-8" />
</head><body><h2>Don't panic!</h2>
  <script>
    document.domain = document.domain;
    var c = parent.testCallback;
    c.start();
    function p(d) {c.message(d);};
    window.onload = function() {c.stop();};
  </script>
`
