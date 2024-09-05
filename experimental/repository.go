package experimental

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	multierror "github.com/hashicorp/go-multierror"

	"github.com/sirupsen/logrus"
	"github.com/dustin/go-humanize"
)

type repositoryData struct {
	name               string
	layers             map[digest]int
	manifests          map[digest]int
	manifestSignatures map[digest][]digest
	tags               map[string]*tagData
	uploads            []string
	lock               sync.Mutex
}

func (r *repositoryData) layerLinkPath(layer digest) string {
	return filepath.Join("repositories", r.name, "_layers", layer.path(), "link")
}

func (r *repositoryData) manifestRevisionPath(revision digest) string {
	return filepath.Join("repositories", r.name, "_manifests", "revisions", revision.path(), "link")
}

func (r *repositoryData) manifestRevisionSignaturePath(revision, signature digest) string {
	return filepath.Join("repositories", r.name, "_manifests", "revisions", revision.path(), "signatures", signature.path(), "link")
}

func (r *repositoryData) uploadPath(upload string) string {
	return filepath.Join("repositories", r.name, "_uploads", upload, "link")
}

func (r *repositoryData) tag(name string) *tagData {
	r.lock.Lock()
	defer r.lock.Unlock()

	t := r.tags[name]
	if t == nil {
		t = &tagData{
			repository: r,
			name:       name,
		}
		r.tags[name] = t
	}

	return t
}

func (r *repositoryData) markManifest(revision digest) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	logrus.Debugln("MANIFEST: marking ", revision)
	r.manifests[revision]++
	return nil
}

func (r *repositoryData) markManifestLayers(blobs blobsData, revision digest) error {
	err := blobs.mark(revision)
	if err != nil {
		return err
	}

	manifest, err := manifests.get(revision, blobs)
	if err != nil {
		return err
	}

	var resultErr error
    for _, m := range manifest.manifests {
        _, ok := r.manifests[m]
        if ok {
            r.markManifest(m)
        }
    }

	r.lock.Lock()
	defer r.lock.Unlock()

	for _, layer := range manifest.layers {
		_, ok := r.layers[layer]
		if !ok {
			resultErr = multierror.Append(resultErr, fmt.Errorf("layer %s not found reference from manifest %s", layer, revision))
		}

		r.layers[layer]++
	}

	return resultErr
}

func (r *repositoryData) markManifestSignatures(blobs blobsData, revision digest, signatures []digest) error {
	if r.manifests[revision] == 0 {
		return nil
	}

	for _, signature := range signatures {
		blobs.mark(signature)
	}
	return nil
}

func (r *repositoryData) sweepManifestSignatures(revision digest, signatures []digest) error {
	if r.manifests[revision] > 0 {
		return nil
	}

	for _, signature := range signatures {
		err := deleteFile(r.manifestRevisionSignaturePath(revision, signature), digestReferenceSize)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *repositoryData) markLayer(blobs blobsData, revision digest) error {
	return blobs.mark(revision)
}

func (r *repositoryData) mark(blobs blobsData) error {
	for name, t := range r.tags {
		err := t.mark(blobs)
		if err != nil {
			if *softErrors {
				logrus.Errorln("MARK:", r.name, "TAG:", name, "ERROR:", err)
				continue
			}
			return err
		}
	}

	for revision, used := range r.manifests {
		if used == 0 {
			continue
		}

		err := r.markManifestLayers(blobs, revision)
		if err != nil {
			if *softErrors {
				logrus.Errorln("MARK:", r.name, "MANIFEST:", revision, "ERROR:", err)
				continue
			}
			return err
		}
	}

	for revision, signatures := range r.manifestSignatures {
		err := r.markManifestSignatures(blobs, revision, signatures)
		if err != nil {
			if *softErrors {
				logrus.Errorln("MARK:", r.name, "MANIFEST SIGNATURE:", revision, "ERROR:", err)
				continue
			}
			return err
		}
	}

	for digest, used := range r.layers {
		if used == 0 {
			continue
		}

		err := r.markLayer(blobs, digest)
		if err != nil {
			if *softErrors {
				logrus.Errorln("MARK:", r.name, "LAYER:", digest, "ERROR:", err)
				continue
			}
			return err
		}
	}
	return nil
}

func (r *repositoryData) sweep() error {
    logrus.Debug("SWEEP: list of manifests", r.manifests)
	for name, t := range r.tags {
		err := t.sweep()
		if err != nil {
			if *softErrors {
				logrus.Errorln("SWEEP:", r.name, "TAG:", name, "ERROR:", err)
				continue
			}
			return err
		}
	}

	for revision, used := range r.manifests {
		if used > 0 {
			continue
		}

		err := deleteFile(r.manifestRevisionPath(revision), digestReferenceSize)
		if err != nil {
			if *softErrors {
				logrus.Errorln("SWEEP:", r.name, "MANIFEST:", revision, "ERROR:", err)
				continue
			}
			return err
		}
	}

	for revision, signatures := range r.manifestSignatures {
		err := r.sweepManifestSignatures(revision, signatures)
		if err != nil {
			if *softErrors {
				logrus.Errorln("MARK:", r.name, "MANIFEST SIGNATURES:", revision, "ERROR:", err)
				continue
			}
			return err
		}
	}

	for digest, used := range r.layers {
		if used > 0 {
			continue
		}

		err := deleteFile(r.layerLinkPath(digest), digestReferenceSize)
		if err != nil {
			if *softErrors {
				logrus.Errorln("MARK:", r.name, "LAYER:", digest, "ERROR:", err)
				continue
			}
			return err
		}
	}

	return nil
}

func (r *repositoryData) addLayer(args []string, info fileInfo) error {
	// /test/_layers/sha256/579c7fc9b0d60a19706cd6c1573fec9a28fa758bfe1ece86a1e5c68ad6f4e9d1/link
	link, err := analyzeLink(args)
	if err != nil {
		return err
	}

	err = verifyLink(link, r.layerLinkPath(link), info.etag)
	if err != nil {
		return err
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	r.layers[link] = 0
	return nil
}

func (r *repositoryData) addManifestRevision(args []string, info fileInfo) error {
	// /test2/_manifests/revisions/sha256/708519982eae159899e908639f5fa22d23d247ad923f6e6ad6128894c5d497a0/link
	link, err := analyzeLink(args)
	if err == nil {
		err = verifyLink(link, r.manifestRevisionPath(link), info.etag)
		if err != nil {
			return err
		}

		r.lock.Lock()
		defer r.lock.Unlock()

		r.manifests[link] = 0
		return nil
	}

	link, signature, err := analyzeLinkSignature(args)
	if err == nil {
		err = verifyLink(signature, r.manifestRevisionSignaturePath(link, signature), info.etag)
		if err != nil {
			return err
		}

		r.lock.Lock()
		defer r.lock.Unlock()

		r.manifestSignatures[link] = append(r.manifestSignatures[link], signature)
		return nil
	}
	return err
}

func (r *repositoryData) addTag(args []string, info fileInfo) error {
	//INFO[0000] /test2/_manifests/tags/latest/current/link
	//INFO[0000] /test2/_manifests/tags/latest/index/sha256/af8338145978acd626bfb9e863fa446bebfc9f2660bee1af99ed29efc48d73b4/link

	tag := r.tag(args[0])
	if args[1] == "current" {
		return tag.setCurrent(info)
	} else if args[1] == "index" {
		return tag.addVersion(args[2:], info)
	} else {
		return fmt.Errorf("undefined manifest tag type: %v", args[1])
	}
}

func (r *repositoryData) addManifest(args []string, info fileInfo) error {
	//INFO[0000] /test2/_manifests/revisions/sha256/708519982eae159899e908639f5fa22d23d247ad923f6e6ad6128894c5d497a0/link
	//INFO[0000] /test2/_manifests/revisions/sha256/af8338145978acd626bfb9e863fa446bebfc9f2660bee1af99ed29efc48d73b4/link
	//INFO[0000] /test2/_manifests/tags/latest/current/link
	//INFO[0000] /test2/_manifests/tags/latest/index/sha256/af8338145978acd626bfb9e863fa446bebfc9f2660bee1af99ed29efc48d73b4/link
	//INFO[0000] /test2/_manifests/tags/latest2/current/link
	//INFO[0000] /test2/_manifests/tags/latest2/index/sha256/708519982eae159899e908639f5fa22d23d247ad923f6e6ad6128894c5d497a0/link

	if args[0] == "revisions" {
		return r.addManifestRevision(args[1:], info)
	} else if args[0] == "tags" {
		return r.addTag(args[1:], info)
	} else {
		return fmt.Errorf("undefined manifest type: %v", args[0])
	}
}

func (r *repositoryData) addUpload(args []string, info fileInfo) error {
	// /test/_uploads/f82d2b61-f130-4be5-b4f6-92cb18c7cf89/startedat
	// /test/_uploads/f82d2b61-f130-4be5-b4f6-92cb18c7cf89/hashstates/sha256/0
	if len(args) < 1 {
		return fmt.Errorf("invalid args for uploads: %v", args)
	}

	r.lock.Lock()
	defer r.lock.Unlock()

	r.uploads = append(r.uploads, strings.Join(args, "/"))
	return nil
}

func (r *repositoryData) info(blobs blobsData, stream io.WriteCloser) {
	var layersUsed, layersUnused int
	var manifestsUsed, manifestsUnused int
	var tagsVersions int
	var layersUsedSize, layersUnusedSize int64

	for digest, used := range r.layers {
		if used > 0 {
			layersUsed++
			layersUsedSize += blobs.size(digest)
		} else {
			layersUnused++
			layersUnusedSize += blobs.size(digest)
		}
	}

	for _, used := range r.manifests {
		if used > 0 {
			manifestsUsed++
		} else {
			manifestsUnused++
		}
	}

	for _, tag := range r.tags {
		tagsVersions += len(tag.versions)
	}

	logrus.Println("REPOSITORY INFO:", r.name, ":",
		"Tags/Versions:", len(r.tags), "/", tagsVersions,
		"Manifests/Unused:", manifestsUsed, "/", manifestsUnused,
		"Layers/Unused:", layersUsed, "/", layersUnused,
		"Data/Unused:", humanize.Bytes(uint64(layersUsedSize)), "/", humanize.Bytes(uint64(layersUnusedSize)))

	if stream != nil {
		fmt.Fprintf(stream, "%s,%d,%d,%d,%d,%d,%d,%s,%s,%d,%d\n",
			r.name, len(r.tags), tagsVersions,
			manifestsUsed, manifestsUnused,
			layersUsed, layersUnused,
			humanize.Bytes(uint64(layersUsedSize)), humanize.Bytes(uint64(layersUnusedSize)),
			layersUsedSize/1024/1024, layersUnusedSize/1024/1024)
	}
}

func newRepositoryData(name string) *repositoryData {
	return &repositoryData{
		name:               name,
		layers:             make(map[digest]int),
		manifests:          make(map[digest]int),
		manifestSignatures: make(map[digest][]digest),
		tags:               make(map[string]*tagData),
	}
}
