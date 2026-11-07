package download

import (
	"context"
	"errors"

	"github.com/zu1k/nali/pkg/common"
)

// Download downloads the first of urls that answers and saves it to filePath.
func Download(filePath string, urls ...string) (data []byte, err error) {
	return DownloadWithContext(context.Background(), nil, filePath, urls...)
}

// DownloadWithContext is Download with a context and a progress callback: it
// stops when ctx is done and reports the bytes received through progress, which
// may be nil.
func DownloadWithContext(ctx context.Context, progress common.DownloadProgress, filePath string, urls ...string) (data []byte, err error) {
	if len(urls) == 0 {
		return nil, errors.New("未指定下载 url")
	}

	data, err = common.GetHttpClient().GetWithContext(ctx, progress, urls...)
	if err != nil {
		return
	}

	err = common.SaveFile(filePath, data)
	return
}
