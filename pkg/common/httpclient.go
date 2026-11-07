package common

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/102.0.0.0 Safari/537.36"

// DownloadProgress reports how many bytes of a response have been received.
// total is 0 when the server does not announce a length.
type DownloadProgress func(read, total int64)

const (
	// progressInterval is how often a download reports its progress at most.
	progressInterval = 100 * time.Millisecond

	// progressStep is the number of bytes after which a download reports its
	// progress at most.
	progressStep = 1 << 20
)

type HttpClient struct {
	*http.Client
}

var httpClient *HttpClient

func init() {
	httpClient = &HttpClient{http.DefaultClient}
	httpClient.Timeout = time.Second * 300
	httpClient.Transport = &http.Transport{
		TLSHandshakeTimeout:   time.Second * 5,
		IdleConnTimeout:       time.Second * 10,
		ResponseHeaderTimeout: time.Second * 10,
		ExpectContinueTimeout: time.Second * 20,
		Proxy:                 http.ProxyFromEnvironment,
	}
}

func GetHttpClient() *HttpClient {
	c := *httpClient
	return &c
}

// Get downloads the first of urls that answers 200 OK.
func (c *HttpClient) Get(urls ...string) (body []byte, err error) {
	return c.GetWithContext(context.Background(), nil, urls...)
}

// GetWithContext downloads the first of urls that answers 200 OK, reports
// progress and aborts when ctx is done. The progress callback is throttled so
// a large download does not flood the caller.
func (c *HttpClient) GetWithContext(ctx context.Context, progress DownloadProgress, urls ...string) (body []byte, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var req *http.Request
	var resp *http.Response

	err = errors.New("no download url")
	for _, url := range urls {
		// A cancelled download must not start another request.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			log.Println(err)
			continue
		}
		req.Header.Set("User-Agent", UserAgent)
		resp, err = c.Do(req)
		if err != nil {
			// Report the cancellation itself, not the transport error that
			// merely wraps it.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			err = fmt.Errorf("%s: unexpected HTTP status %s", url, resp.Status)
			continue
		}

		// A missing Content-Length is reported as "unknown", which is 0.
		total := resp.ContentLength
		if total < 0 {
			total = 0
		}
		reader := &progressReader{body: resp.Body, total: total, report: progress}
		body, err = io.ReadAll(reader)
		_ = resp.Body.Close()
		// The final byte count is always reported, so a caller that only looks at
		// the last event sees the exact size even though the updates in between
		// were throttled.
		reader.finish()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if err != nil {
			continue
		}
		return body, nil
	}

	return nil, err
}

// progressReader counts the bytes read from a response body and reports them to
// the caller, throttled so that neither a fast nor a slow download floods it.
type progressReader struct {
	body   io.Reader
	total  int64
	report DownloadProgress

	read     int64
	last     time.Time
	lastRead int64
	reported bool
}

// Read implements io.Reader.
func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.body.Read(b)
	if n > 0 {
		p.read += int64(n)
		p.maybeReport()
	}
	return n, err
}

// maybeReport reports the progress at most once per interval and once per
// progressStep bytes.
func (p *progressReader) maybeReport() {
	if p.report == nil {
		return
	}
	now := time.Now()
	if p.reported && p.read-p.lastRead < progressStep && now.Sub(p.last) < progressInterval {
		return
	}
	p.reported = true
	p.last = now
	p.lastRead = p.read
	p.report(p.read, p.total)
}

// finish reports the final byte count, so the last event always carries the
// exact size, including a download that never got past the throttle.
func (p *progressReader) finish() {
	if p.report == nil {
		return
	}
	p.reported = true
	p.lastRead = p.read
	p.report(p.read, p.total)
}
