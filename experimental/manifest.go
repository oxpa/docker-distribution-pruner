package experimental

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest"
	"github.com/distribution/distribution/manifest/manifestlist"
	"github.com/distribution/distribution/manifest/schema1"
	"github.com/distribution/distribution/manifest/schema2"
	"github.com/distribution/distribution/manifest/ocischema"
	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type manifestData struct {
	digest  digest
	layers  []digest
	manifests []digest
	loaded  bool
	loadErr error

	loadLock sync.Mutex
}

func deserializeManifest(data []byte) (distribution.Manifest, error) {
	var versioned manifest.Versioned
	if err := json.Unmarshal(data, &versioned); err != nil {
		return nil, err
	}

	switch versioned.SchemaVersion {
	case 1:
		var sm schema1.SignedManifest
		err := sm.UnmarshalJSON(data)
		return sm, err
	case 2:
		// This can be an image manifest or a manifest list
		switch versioned.MediaType {
			case schema2.MediaTypeManifest:
				var m schema2.DeserializedManifest
				err := m.UnmarshalJSON(data)
				return m, err
			case ociv1.MediaTypeImageManifest:
				var m ocischema.DeserializedManifest
				err := m.UnmarshalJSON(data)
				return m, err
			case ociv1.MediaTypeImageIndex:
				var m manifestlist.DeserializedManifestList
				err := m.UnmarshalJSON(data)
				return m, err
			case manifestlist.MediaTypeManifestList:
				var m manifestlist.DeserializedManifestList
				err := m.UnmarshalJSON(data)
				return m, err
			default:
				return nil, distribution.ErrManifestVerification{fmt.Errorf("unrecognized manifest content type %s", versioned.MediaType)}
		}
	}

	return nil, fmt.Errorf("unrecognized manifest schema version %d", versioned.SchemaVersion)
}

func (m *manifestData) path() string {
	return filepath.Join("blobs", m.digest.scopedPath(), "data")
}

func (m *manifestData) load(blobs blobsData) error {
	logrus.Infoln("MANIFEST:", m.path(), ": loading...")

	data, err := currentStorage.Read(m.path(), blobs.etag(m.digest))
	if err != nil {
		return err
	}

	manifest, err := deserializeManifest(data)
	if err != nil {
		return err
	}
	switch manifest.(type) {
		case manifestlist.DeserializedManifestList:
			for _, reference := range manifest.References() {
				//TODO: recursively call the same function to avoid code duplication?
				digest, err := newDigestFromReference([]byte(reference.Digest))
				if err != nil {
					return err
				}
				logrus.Infoln("MANIFEST: list", m.path(), " references ", digest)
				m.manifests = append(m.manifests, digest)

				path := filepath.Join("blobs", digest.scopedPath(), "data")
				data, err = currentStorage.Read(path, blobs.etag(digest))
				manifest, err = deserializeManifest(data)

				if err != nil {
					return err
				}

				for _, reference := range manifest.References() {
					digest, err := newDigestFromReference([]byte(reference.Digest))
					if err != nil {
						return err
					}
					m.layers = append(m.layers, digest)
				}

			}

		default:
			for _, reference := range manifest.References() {
				digest, err := newDigestFromReference([]byte(reference.Digest))
				if err != nil {
					return err
				}
				m.layers = append(m.layers, digest)
			}
	}
	return nil
}

func (m *manifestData) ensureLoaded(blobs blobsData) error {
	if !m.loaded {
		m.loadLock.Lock()
		defer m.loadLock.Unlock()

		if !m.loaded {
			m.loadErr = m.load(blobs)
			m.loaded = true
		}
	}

	return m.loadErr
}
