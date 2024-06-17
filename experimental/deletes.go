package experimental

import (
	"path/filepath"
	"sync/atomic"

	"github.com/sirupsen/logrus"
	"github.com/dustin/go-humanize"
)

var (
	deletedLinks    int32
	deletedBlobs    int32
	deletedOther    int32
	deletedBlobSize int64
)

func deleteFile(path string, size int64) error {
	logrus.Infoln("DELETE", path, size)
	name := filepath.Base(path)
	if name == "link" {
		atomic.AddInt32(&deletedLinks, 1)
	} else if name == "data" {
		atomic.AddInt32(&deletedBlobs, 1)
	} else {
		atomic.AddInt32(&deletedOther, 1)
	}

	atomic.AddInt64(&deletedBlobSize, size)

	if !*delete {
		// Do not delete, only write
		return nil
	}

	if *softDelete {
		return currentStorage.Move(path, filepath.Join("backup", path))
	} else {
		return currentStorage.Delete(path)
	}
}

func deletesInfo() {
	logrus.Warningln("DELETEABLE INFO:", deletedLinks, "links,",
		deletedBlobs, "blobs,",
		deletedOther, "other,",
		humanize.Bytes(uint64(deletedBlobSize)),
	)
}
