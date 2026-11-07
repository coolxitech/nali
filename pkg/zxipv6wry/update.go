package zxipv6wry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/saracen/go7z"
	"github.com/zu1k/nali/pkg/common"
)

// Download downloads and unpacks the ZX IPv6 database, optionally saving it to
// filePath.
func Download(filePath ...string) (data []byte, err error) {
	return DownloadWithContext(context.Background(), nil, filePath...)
}

// DownloadWithContext is Download with a context and a progress callback: it
// stops when ctx is done and reports the bytes received through progress, which
// may be nil.
func DownloadWithContext(ctx context.Context, progress common.DownloadProgress, filePath ...string) (data []byte, err error) {
	data, err = getData(ctx, progress)
	if err != nil {
		log.Printf("ZX IPv6数据库下载失败，请手动下载解压后保存到本地: %s \n", filePath)
		log.Println("下载链接： https://ip.zxinc.org/ip.7z")
		return
	}

	if !CheckFile(data) {
		log.Printf("ZX IPv6数据库下载出错，请手动下载解压后保存到本地: %s \n", filePath)
		log.Println("下载链接： https://ip.zxinc.org/ip.7z")
		return nil, errors.New("数据库下载内容出错")
	}

	if len(filePath) == 1 {
		if err := common.SaveFile(filePath[0], data); err == nil {
			log.Println("已将最新的 ZX IPv6数据库 保存到本地:", filePath)
		}
	}
	return
}

const (
	zx = "https://ip.zxinc.org/ip.7z"
)

func getData(ctx context.Context, progress common.DownloadProgress) (data []byte, err error) {
	data, err = common.GetHttpClient().GetWithContext(ctx, progress, zx)
	if err != nil {
		return nil, err
	}

	file7z, err := os.CreateTemp("", "*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file7z.Name())
	if err := os.WriteFile(file7z.Name(), data, 0644); err == nil {
		return Un7z(ctx, file7z.Name())
	}
	return
}

// Un7z unpacks the database member of the 7z archive at filePath. It stops with
// the error of ctx when ctx is done, so a cancelled update does not keep
// decompressing.
func Un7z(ctx context.Context, filePath string) (data []byte, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sz, err := go7z.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer sz.Close()

	fileNoNeed, err := os.CreateTemp("", "*")
	if err != nil {
		return nil, err
	}
	// The temporary files are closed before they are removed (defers run in
	// reverse order, so the Close below must come last), which Windows requires,
	// and this happens on every return path, including the decompression errors
	// reported further down.
	defer os.Remove(fileNoNeed.Name())
	defer fileNoNeed.Close()
	fileNeed, err := os.CreateTemp("", "*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(fileNeed.Name())
	defer fileNeed.Close()

	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		hdr, err := sz.Next()
		if err == io.EOF {
			break // IdxEnd of archive
		}
		if err != nil {
			return nil, err
		}

		if hdr.Name == "ipv6wry.db" {
			if _, err := io.Copy(fileNeed, sz); err != nil {
				return nil, fmt.Errorf("ZX ipv6数据库解压出错：%w", err)
			}
		} else {
			if _, err := io.Copy(fileNoNeed, sz); err != nil {
				return nil, fmt.Errorf("ZX ipv6数据库解压出错：%w", err)
			}
		}
	}
	err = fileNoNeed.Close()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(fileNeed.Name())
}
