package experimental

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

type repositoriesData map[string]*repositoryData

var repositoriesLock sync.Mutex

func (r repositoriesData) get(path []string) *repositoryData {
	repositoryName := strings.Join(path, "/")

	repositoriesLock.Lock()
	defer repositoriesLock.Unlock()

	repository := r[repositoryName]
	if repository == nil {
		repository = newRepositoryData(repositoryName)
		r[repositoryName] = repository
	}

	return repository
}

func (r repositoriesData) process(segments []string, info fileInfo) error {
	for idx := 0; idx < len(segments)-1; idx++ {
		repository := segments[0:idx]
		args := segments[idx+1:]

		switch segments[idx] {
		case "_layers":
			return r.get(repository).addLayer(args, info)

		case "_manifests":
			return r.get(repository).addManifest(args, info)

		case "_uploads":
			return r.get(repository).addUpload(args, info)
		}
	}

	return fmt.Errorf("unparseable path: %v", segments)
}

func (r repositoriesData) walkPath(walkPath string, jg *jobGroup) error {
	logrus.Infoln("REPOSITORIES DIR:", walkPath)
	return currentStorage.Walk(walkPath, "repositories", func(path string, info fileInfo, err error) error {
		jg.dispatch(func() error {
			err = r.process(strings.Split(path, "/"), info)
			if err != nil {
				if err != nil {
					logrus.Errorln("REPOSITORY:", path, ":", err)
					if *softErrors {
						return nil
					}
				} else {
					logrus.Infoln("REPOSITORY:", path, ":", err)
				}
				return err
			}
			return nil
		})
		return nil
	})
}

func (r repositoriesData) walk(parallel bool) error {
	logrus.Infoln("Walking REPOSITORIES...")

	jg := jobsRunner.group()

	if parallel {
		err := parallelWalk("repositories", func(listPath string) error {
			return r.walkPath(listPath, jg)
		})
		if err != nil {
			return err
		}
	} else {
		err := r.walkPath("repositories", jg)
		if err != nil {
			return err
		}
	}

	return jg.finish()
}

func (r repositoriesData) mark(blobs blobsData) error {
	jg := jobsRunner.group()

	for _, repository_ := range r {
		repository := repository_
		jg.dispatch(func() error {
			return repository.mark(blobs)
		})
	}

	err := jg.finish()
	if err != nil {
		return err
	}
	return nil
}

func (r repositoriesData) sweep() error {
	jg := jobsRunner.group()

	for _, repository_ := range r {
		repository := repository_
		jg.dispatch(func() error {
			return repository.sweep()
		})
	}

	err := jg.finish()
	if err != nil {
		return err
	}
	return nil
}

func (r repositoriesData) info(blobs blobsData, csvOutput string) {
	var stream io.WriteCloser

	if csvOutput != "" {
		var err error
		stream, err = os.Create(csvOutput)
		if err == nil {
			defer stream.Close()

			labels := []string{
				"Repository",
				"Tags",
				"TagVersions",
				"Manifests",
				"ManifestsUnused",
				"Layers",
				"LayersUnused",
				"Data",
				"DataUnused",
				"Data-MB",
				"DataUnused-MB",
			}

			fmt.Fprintln(stream, strings.Join(labels, ","))
		} else {
			logrus.Warningln(err)
		}
	}

	for _, repository := range r {
		repository.info(blobs, stream)
	}
}
