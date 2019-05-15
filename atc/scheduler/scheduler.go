package scheduler

import (
	"time"

	"code.cloudfoundry.org/lager"
	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/db"
	"github.com/concourse/concourse/atc/scheduler/algorithm"
)

type Scheduler struct {
	InputMapper  algorithm.InputMapper
	BuildStarter BuildStarter
}

func (s *Scheduler) Schedule(
	logger lager.Logger,
	versions *db.VersionsDB,
	job db.Job,
	resources db.Resources,
	resourceTypes atc.VersionedResourceTypes,
) (map[string]time.Duration, error) {
	jobSchedulingTime := map[string]time.Duration{}

	jStart := time.Now()

	inputMapping, resolved, err := s.InputMapper.MapInputs(versions, job, resources)
	if err != nil {
		return jobSchedulingTime, err
	}

	err = job.SaveNextInputMapping(inputMapping, resolved)
	if err != nil {
		logger.Error("failed-to-save-next-input-mapping", err)
		return jobSchedulingTime, err
	}

	err = s.ensurePendingBuildExists(logger, job, resources)
	jobSchedulingTime[job.Name()] = time.Since(jStart)

	if err != nil {
		return jobSchedulingTime, err
	}

	err = s.BuildStarter.TryStartPendingBuildsForJob(logger, job, resources, resourceTypes)
	jobSchedulingTime[job.Name()] = jobSchedulingTime[job.Name()] + time.Since(jStart)

	if err != nil {
		return jobSchedulingTime, err
	}

	return jobSchedulingTime, nil
}

func (s *Scheduler) ensurePendingBuildExists(
	logger lager.Logger,
	job db.Job,
	resources db.Resources,
) error {
	buildInputs, found, err := job.GetFullNextBuildInputs()
	if err != nil {
		logger.Error("failed-to-fetch-next-build-inputs", err)
		return err
	}

	if !found {
		// XXX: better info log pls
		logger.Info("next-build-inputs-not-found")
		return nil
	}

	var inputMapping map[string]db.BuildInput
	for _, input := range buildInputs {
		inputMapping[input.Name] = input
	}

	for _, inputConfig := range job.Config().Inputs() {
		inputSource, ok := inputMapping[inputConfig.Name]

		//trigger: true, and the version has not been used
		if ok && inputSource.FirstOccurrence && inputConfig.Trigger {
			err := job.EnsurePendingBuildExists()
			if err != nil {
				logger.Error("failed-to-ensure-pending-build-exists", err)
				return err
			}

			break
		}
	}

	return nil
}
