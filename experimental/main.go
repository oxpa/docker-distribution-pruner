package experimental

import (
	"flag"
	"os"
	"os/signal"
	"sync"

	"github.com/sirupsen/logrus"
)

var (
	config = flag.String("config", "", "Path to registry config file")

	ignoreBlobs = flag.Bool("ignore-blobs", false, "Ignore blobs processing and recycling")

	jobs             = flag.Int("jobs", 10, "Number of concurrent jobs to execute")
	parallelWalkJobs = flag.Int("parallel-walk-jobs", 10, "Number of concurrent parallel walk jobs to execute")

	trace      = flag.Bool("trace", false, "Print trace messages")
	debug      = flag.Bool("debug", false, "Print debug messages")
	verbose    = flag.Bool("verbose", true, "Print verbose messages")
	softErrors = flag.Bool("soft-errors", false, "Print errors, but do not fail")

	parallelRepositoryWalk = flag.Bool("parallel-repository-walk", false, "Allow to use parallel repository walker")
	parallelBlobWalk       = flag.Bool("parallel-blob-walk", false, "Allow to use parallel blob walker")

	repositoryCsvOutput = flag.String("repository-csv-output", "repositories.csv", "File to which CSV will be written with all metrics")

	deleteOldTagVersions = flag.Bool("delete-old-tag-versions", true, "Delete old tag versions")
	delete               = flag.Bool("delete", false, "Delete data, instead of dry run")
	softDelete           = flag.Bool("soft-delete", true, "When deleting, do not remove, but move to backup/ folder")
)

var (
	jobsRunner         = make(jobsData)
	parallelWalkRunner = make(jobsData)
)

func logErrorln(args ...interface{}) {
	if *softErrors {
		logrus.Errorln(args...)
	} else {
		logrus.Fatalln(args...)
	}
}

func Main() {
	flag.Parse()

    if *trace {
		logrus.SetLevel(logrus.TraceLevel)
    } else if *debug {
		logrus.SetLevel(logrus.DebugLevel)
	} else if *verbose {
		logrus.SetLevel(logrus.InfoLevel)
	} else {
		logrus.SetLevel(logrus.WarnLevel)
	}

	logrus.SetFormatter(&logrus.TextFormatter{ForceColors: true})

	if *config == "" {
		flag.Usage()
		os.Exit(1)
	}

	var err error
	currentStorage, err = storageFromConfig(*config)
	if err != nil {
		logrus.Fatalln(err)
	}

	blobs := make(blobsData)
	repositories := make(repositoriesData)

	jobsRunner.run(*jobs)
	parallelWalkRunner.run(*parallelWalkJobs)

	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt, os.Kill)

	go func() {
		for signal := range signals {
			currentStorage.Info()
			logrus.Fatalln("Signal received:", signal)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		err := repositories.walk(*parallelRepositoryWalk)
		if err != nil {
			logErrorln(err)
		}
	}()

	go func() {
		defer wg.Done()

		if *ignoreBlobs {
			return
		}

		err := blobs.walk(*parallelBlobWalk)
		if err != nil {
			logErrorln(err)
		}
	}()

	wg.Wait()

	logrus.Infoln("Marking REPOSITORIES...")
	err = repositories.mark(blobs)
	if err != nil {
		logErrorln(err)
	}

	logrus.Infoln("Sweeping REPOSITORIES...")
	err = repositories.sweep()
	if err != nil {
		logErrorln(err)
	}

	logrus.Infoln("Sweeping BLOBS...")
	err = blobs.sweep()
	if err != nil {
		logErrorln(err)
	}

	logrus.Infoln("Summary...")
	repositories.info(blobs, *repositoryCsvOutput)
	blobs.info()
	deletesInfo()
	currentStorage.Info()
}
