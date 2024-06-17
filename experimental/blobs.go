package experimental

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/dustin/go-humanize"
)

type blobsData map[digest]*blobData

var blobsLock sync.Mutex

func (b blobsData) mark(digest digest) error {
	if *ignoreBlobs {
		return nil
	}

	blobsLock.Lock()
	defer blobsLock.Unlock()

	blob := b[digest]
	if blob == nil {
		return fmt.Errorf("blob not found: %v", digest)
	}
	blob.references++
	return nil
}

func (b blobsData) etag(digest digest) string {
	blob := b[digest]
	if blob != nil {
		return blob.etag
	}
	return ""
}

func (b blobsData) size(digest digest) int64 {
	blob := b[digest]
	if blob != nil {
		return blob.size
	}
	return 0
}

func (b blobsData) sweep() error {
	jg := jobsRunner.group()

	for _, blob_ := range b {
		blob := blob_
		jg.dispatch(func() error {
			if blob.references > 0 {
				return nil
			}

			err := deleteFile(blob.path(), blob.size)
			if err != nil {
				return err
			}
			return nil
		})
	}

	return jg.finish()
}

func (b blobsData) addBlob(segments []string, info fileInfo) error {
	if len(segments) != 4 {
		return fmt.Errorf("unparseable path: %v", segments)
	}

	if segments[3] != "data" {
		return fmt.Errorf("file needs to be data: %v", segments)
	}

	digest, err := newDigestFromScopedPath(segments[0:3])
	if err != nil {
		return err
	}

	if segments[0] != "sha256" {
		return fmt.Errorf("path needs to start with sha256: %v", segments)
	}

	blobsLock.Lock()
	defer blobsLock.Unlock()

	blob := &blobData{
		name: digest,
		size: info.size,
		etag: info.etag,
	}
	b[digest] = blob
	return nil
}

func (b blobsData) walkPath(walkPath string) error {
	logrus.Infoln("BLOBS DIR:", walkPath)
	return currentStorage.Walk(walkPath, "blobs", func(path string, info fileInfo, err error) error {
		err = b.addBlob(strings.Split(path, "/"), info)
		if err != nil {
			logrus.Errorln("BLOB:", path, ":", err)
			if *softErrors {
				return nil
			}
		} else {
			logrus.Infoln("BLOB:", path, ":", err)
		}
		return err
	})
}

func (b blobsData) walk(parallel bool) error {
	logrus.Infoln("Walking BLOBS...")

	if parallel {
		listRootPath := filepath.Join("blobs", "sha256")
		return parallelWalk(listRootPath, b.walkPath)
	} else {
		return b.walkPath("blobs")
	}
}

func (b blobsData) info() {
	var blobsUsed, blobsUnused int
	var blobsUsedSize, blobsUnusedSize int64

	if *ignoreBlobs {
		return
	}

	for _, blob := range b {
		if blob.references > 0 {
			blobsUsed++
			blobsUsedSize += blob.size
		} else {
			blobsUnused++
			blobsUnusedSize += blob.size
		}
	}

	logrus.Infoln("BLOBS INFO:",
		"Objects/Unused:", blobsUsed, "/", blobsUnused,
		"Data/Unused:", humanize.Bytes(uint64(blobsUsedSize)), "/", humanize.Bytes(uint64(blobsUnusedSize)),
	)
}
