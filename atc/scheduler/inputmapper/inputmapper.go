package inputmapper

import (
	"code.cloudfoundry.org/lager"
	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/db"
	"github.com/concourse/concourse/atc/scheduler/algorithm"
	"github.com/concourse/concourse/atc/scheduler/inputmapper/inputconfig"
)

//go:generate counterfeiter . InputMapper

type InputMapper interface {
	SaveNextInputMapping(
		logger lager.Logger,
		versions *algorithm.VersionsDB,
		job db.Job,
		resources db.Resources,
	) (algorithm.InputMapping, error)
}

func NewInputMapper(pipeline db.Pipeline, transformer inputconfig.Transformer) InputMapper {
	return &inputMapper{pipeline: pipeline, transformer: transformer}
}

type inputMapper struct {
	pipeline    db.Pipeline
	transformer inputconfig.Transformer
}

func (i *inputMapper) SaveNextInputMapping(
	logger lager.Logger,
	versions *algorithm.VersionsDB,
	job db.Job,
	resources db.Resources,
) error {
	logger = logger.Session("save-next-input-mapping")

	inputConfigs := job.Config().Inputs()

	for i, inputConfig := range inputConfigs {
		resource, found := resources.Lookup(inputConfig.Resource)

		if !found {
			logger.Debug("failed-to-find-resource")
			continue
		}

		if resource.CurrentPinnedVersion() != nil {
			inputConfigs[i].Version = &atc.VersionConfig{Pinned: resource.CurrentPinnedVersion()}
		}
	}

	algorithmInputConfigs, err := i.transformer.TransformInputConfigs(versions, job.Name(), inputConfigs)
	if err != nil {
		logger.Error("failed-to-get-algorithm-input-configs", err)
		return err
	}

	mapping, ok, err = algorithmInputConfigs.ComputeNextInputs(versions)
	if err != nil {
		logger.Error("failed-to-resolve-inputs", err)
		return err
	}

	err = job.SaveNextInputMapping(mapping, ok)
	if err != nil {
		logger.Error("failed-to-save-next-input-mapping", err)
		return err
	}

	return nil
}
