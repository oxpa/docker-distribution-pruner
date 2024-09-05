package experimental

import (
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"
)

type tagData struct {
	repository *repositoryData
	name       string
	current    digest
	versions   []digest
	lock       sync.Mutex
}

func (t *tagData) currentLinkPath() string {
	return filepath.Join("repositories", t.repository.name, "_manifests", "tags", t.name, "current", "link")
}

func (t *tagData) versionLinkPath(version digest) string {
	return filepath.Join("repositories", t.repository.name, "_manifests", "tags", t.name, "index", version.path(), "link")
}

func (t *tagData) mark(blobs blobsData) error {
	if t.current.valid() {
		t.repository.markManifest(t.current)
	}

	for _, version := range t.versions {
		if version == t.current {
			continue
		}

		if *deleteOldTagVersions {
			continue
		}

		t.repository.markManifest(version)
	}

	return nil
}

func (t *tagData) sweep() error {
	if !t.current.valid() {
        logrus.Trace("TAG: deleting invalid current ", t.currentLinkPath())
		err := deleteFile(t.currentLinkPath(), digestReferenceSize)
		if err != nil {
			return err
		}
	}

	for _, version := range t.versions {
		if version == t.current {
			continue
		}

		if t.repository.manifests[version] > 0 {
			continue
		}

		if *deleteOldTagVersions {
            logrus.Trace("TAG: deleting old version", version)
			err := deleteFile(t.versionLinkPath(version), digestReferenceSize)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *tagData) setCurrent(info fileInfo) error {
	//INFO[0000] /test2/_manifests/tags/latest/current/link

	link, err := readLink(t.currentLinkPath(), info.etag)
	if err != nil {
		return err
	}

	t.current = link
	logrus.Infoln("TAG:", t.repository.name, ":", t.name, ": is using:", t.current)
	return nil
}

func (t *tagData) addVersion(args []string, info fileInfo) error {
	//INFO[0000] /test2/_manifests/tags/latest/index/sha256/af8338145978acd626bfb9e863fa446bebfc9f2660bee1af99ed29efc48d73b4/link

	link, err := analyzeLink(args)
	if err != nil {
		return err
	}

	err = verifyLink(link, t.versionLinkPath(link), info.etag)
	if err != nil {
		return err
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	t.versions = append(t.versions, link)
	return nil
}
