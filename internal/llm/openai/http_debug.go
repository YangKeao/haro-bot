package openai

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/YangKeao/haro-bot/internal/logging"
	"go.uber.org/zap"
)

const defaultHTTPDebugMaxBody int64 = 1 << 20

type debugTransport struct {
	base      http.RoundTripper
	maxBody   int64
	log       *zap.Logger
	reqSeqNum uint64
}

func newDebugTransport(base http.RoundTripper, maxBody int64) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if maxBody <= 0 {
		maxBody = defaultHTTPDebugMaxBody
	}
	return &debugTransport{
		base:    base,
		maxBody: maxBody,
		log:     logging.L().Named("llm_http"),
	}
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return t.base.RoundTrip(req)
	}
	id := atomic.AddUint64(&t.reqSeqNum, 1)
	start := time.Now()
	reqBody, err := readRequestBody(req)
	if err != nil {
		t.log.Warn("llm http request body read failed", zap.Uint64("id", id), zap.Error(err))
	}
	t.log.Info("llm http request",
		zap.Uint64("id", id),
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
		zap.Any("headers", sanitizeHeaders(req.Header)),
		zap.String("body", truncateBody(reqBody, t.maxBody)),
	)

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.log.Info("llm http response error",
			zap.Uint64("id", id),
			zap.Duration("latency", time.Since(start)),
			zap.Error(err),
		)
		return nil, err
	}
	t.log.Info("llm http response",
		zap.Uint64("id", id),
		zap.Int("status", resp.StatusCode),
		zap.String("status_text", resp.Status),
		zap.Any("headers", sanitizeHeaders(resp.Header)),
		zap.Duration("latency", time.Since(start)),
	)
	if resp.Body != nil {
		resp.Body = &debugReadCloser{
			id:      id,
			rc:      resp.Body,
			log:     t.log,
			maxBody: t.maxBody,
		}
	}
	return resp, nil
}

type debugReadCloser struct {
	id      uint64
	rc      io.ReadCloser
	buf     bytes.Buffer
	log     *zap.Logger
	maxBody int64
	closed  atomic.Bool
	trunc   bool
}

func (d *debugReadCloser) Read(p []byte) (int, error) {
	n, err := d.rc.Read(p)
	if n > 0 && d.maxBody > 0 && int64(d.buf.Len()) < d.maxBody {
		remaining := d.maxBody - int64(d.buf.Len())
		toCopy := n
		if int64(toCopy) > remaining {
			toCopy = int(remaining)
			d.trunc = true
		}
		_, _ = d.buf.Write(p[:toCopy])
	} else if n > 0 && d.maxBody > 0 && int64(d.buf.Len()) >= d.maxBody {
		d.trunc = true
	}
	return n, err
}

func (d *debugReadCloser) Close() error {
	if d.closed.Swap(true) {
		return d.rc.Close()
	}
	body := d.buf.String()
	if body != "" && d.trunc {
		body = body + "...(truncated)"
	}
	d.log.Info("llm http response body",
		zap.Uint64("id", d.id),
		zap.String("body", body),
	)
	return d.rc.Close()
}

func readRequestBody(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func truncateBody(body []byte, maxBody int64) string {
	if maxBody <= 0 || int64(len(body)) <= maxBody {
		return string(body)
	}
	return string(body[:maxBody]) + "...(truncated)"
}

func sanitizeHeaders(h http.Header) map[string][]string {
	if h == nil {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, v := range h {
		if isSensitiveHeader(k) {
			out[k] = []string{"REDACTED"}
			continue
		}
		out[k] = v
	}
	return out
}

func isSensitiveHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization", "x-api-key", "x-subscription-token":
		return true
	default:
		return false
	}
}
