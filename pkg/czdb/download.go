package czdb

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/zu1k/nali/pkg/common"
)

const (
	// downloadRetries is how many times a failed download is retried, so one
	// flaky response does not abort a database update. The overall timeout is
	// enforced by common.GetHttpClient.
	downloadRetries = 3

	// zipMagic is the local file header signature of a zip archive.
	zipMagic = "PK\x03\x04"
)

var (
	// payloadMu guards payloadCache.
	payloadMu sync.Mutex

	// payloadCache keeps the raw download of every download source, because the
	// IPv4 and the IPv6 database are two members of the same zip archive and
	// must not be downloaded twice in one process.
	payloadCache = map[string][]byte{}
)

// FetchEntry downloads the archive from urls (empty means []string{DownloadUrl}),
// returns the content of entry (empty means EntryIPv4) after validation, and
// caches the raw payload per download source so IPv4 and IPv6 share one
// download. Only a payload that looks like a database (or an archive) is
// cached, so an error response is retried by the next call.
func FetchEntry(entry string, urls []string) ([]byte, error) {
	if entry == "" {
		entry = EntryIPv4
	}
	if len(urls) == 0 {
		if DownloadUrl == "" {
			return nil, errors.New("未配置 czdb 下载地址")
		}
		urls = []string{DownloadUrl}
	}

	expanded, err := ExpandURLs(urls)
	if err != nil {
		return nil, err
	}
	if len(expanded) == 0 {
		return nil, errors.New("未配置 czdb 下载地址")
	}

	payloadMu.Lock()
	defer payloadMu.Unlock()

	key := strings.Join(expanded, "\n")
	payload, cached := payloadCache[key]
	if !cached {
		payload, err = download(expanded)
		if err != nil {
			return nil, err
		}
		if !isZip(payload) && !CheckFile(payload) {
			return nil, describePayload(payload)
		}
		payloadCache[key] = payload
	}

	data, err := extractDatabase(payload, entry)
	if err != nil {
		return nil, err
	}
	if !CheckFile(data) {
		return nil, fmt.Errorf("纯真 CZDB 文件校验失败（%s 不是有效的 czdb 数据库）", entry)
	}
	// A database of the other family parses just as well, but every query of the
	// requested family would come back as a plain miss, so it must be refused
	// here where the reason is still known.
	ipv6 := entry == EntryIPv6
	if !CheckFileFor(data, ipv6) {
		family := "IPv4"
		if ipv6 {
			family = "IPv6"
		}
		return nil, fmt.Errorf("下载到的数据库不是 %s 库（条目 %s），请检查 download-urls 或 zip-entry", family, entry)
	}
	return data, nil
}

// download fetches the first URL that answers, retrying a few times because the
// official endpoint is occasionally slow or flaky.
func download(urls []string) ([]byte, error) {
	var err error
	for attempt := 0; attempt <= downloadRetries; attempt++ {
		var data []byte
		if data, err = common.GetHttpClient().Get(urls...); err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("纯真 CZDB 下载失败: %w", redactDownloadKey(err))
}

// redactDownloadKey replaces every occurrence of the download key in err with
// "***". The key travels in the query string, and the HTTP client quotes the
// whole URL in its status error, so without this the key would end up in the
// log of a failed update.
func redactDownloadKey(err error) error {
	if err == nil {
		return nil
	}
	key := ResolveDownloadKey()
	if key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), key, "***"))
}

// extractDatabase turns a response body into a czdb database: a zip archive is
// unpacked, a bare database (a custom mirror) is used as is, and anything else
// is reported with the message of the JSON error body the endpoint returns on
// failure.
func extractDatabase(payload []byte, entry string) ([]byte, error) {
	if isZip(payload) {
		return extractZipEntry(payload, entry)
	}
	if CheckFile(payload) {
		return payload, nil
	}
	return nil, describePayload(payload)
}

// describePayload explains why a response body is not usable.
func describePayload(payload []byte) error {
	if message := jsonMessage(payload); message != "" {
		return fmt.Errorf("纯真 CZDB 下载失败: %s", message)
	}
	if len(payload) == 0 {
		return errors.New("纯真 CZDB 下载失败: 服务器返回了空响应")
	}
	return fmt.Errorf("纯真 CZDB 下载失败: 服务器返回了无法识别的响应（%d 字节，开头为 %q）",
		len(payload), preview(payload))
}

// preview renders the first bytes of a response for error messages.
func preview(payload []byte) string {
	const maxPreview = 32
	if len(payload) > maxPreview {
		payload = payload[:maxPreview]
	}
	return strings.ToValidUTF8(string(payload), "?")
}

// jsonMessage extracts the "message" field of a JSON response body, which is
// how the download endpoint reports a missing or invalid authorization key
// (always with HTTP status 200).
func jsonMessage(payload []byte) string {
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.Message)
}

// isZip reports whether payload starts with the zip local file header magic.
func isZip(payload []byte) bool {
	return len(payload) >= len(zipMagic) && string(payload[:len(zipMagic)]) == zipMagic
}

// extractZipEntry returns the content of the named member of a zip archive.
func extractZipEntry(payload []byte, entry string) ([]byte, error) {
	if entry == "" {
		entry = EntryIPv4
	}

	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("解析纯真 CZDB 压缩包失败: %w", err)
	}

	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		if file.Name != entry {
			names = append(names, file.Name)
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("读取压缩包条目 %s 失败: %w", entry, err)
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("解压压缩包条目 %s 失败: %w", entry, readErr)
		}
		return data, nil
	}
	return nil, fmt.Errorf("压缩包中不存在条目 %s（可用条目: %s）", entry, strings.Join(names, ", "))
}
