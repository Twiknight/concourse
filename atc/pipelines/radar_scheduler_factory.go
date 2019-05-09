package pipelines

import (
	"time"

	"code.cloudfoundry.org/clock"
	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/atc/creds"
	"github.com/concourse/concourse/atc/db"
	"github.com/concourse/concourse/atc/engine"
	"github.com/concourse/concourse/atc/radar"
	"github.com/concourse/concourse/atc/resource"
	"github.com/concourse/concourse/atc/scheduler"
	"github.com/concourse/concourse/atc/scheduler/algorithm"
	"github.com/concourse/concourse/atc/scheduler/factory"
	"github.com/concourse/concourse/atc/scheduler/maxinflight"
	"github.com/concourse/concourse/atc/worker"
)

//go:generate counterfeiter . RadarSchedulerFactory

type RadarSchedulerFactory interface {
	BuildScanRunnerFactory(dbPipeline db.Pipeline, externalURL string, variables creds.Variables) radar.ScanRunnerFactory
	BuildScheduler() scheduler.BuildScheduler
}

type radarSchedulerFactory struct {
	pool                         worker.Pool
	resourceFactory              resource.ResourceFactory
	resourceConfigFactory        db.ResourceConfigFactory
	resourceTypeCheckingInterval time.Duration
	resourceCheckingInterval     time.Duration
	engine                       engine.Engine
	strategy                     worker.ContainerPlacementStrategy
}

func NewRadarSchedulerFactory(
	pool worker.Pool,
	resourceFactory resource.ResourceFactory,
	resourceConfigFactory db.ResourceConfigFactory,
	resourceTypeCheckingInterval time.Duration,
	resourceCheckingInterval time.Duration,
	engine engine.Engine,
	strategy worker.ContainerPlacementStrategy,
) RadarSchedulerFactory {
	return &radarSchedulerFactory{
		pool:                         pool,
		resourceFactory:              resourceFactory,
		resourceConfigFactory:        resourceConfigFactory,
		resourceTypeCheckingInterval: resourceTypeCheckingInterval,
		resourceCheckingInterval:     resourceCheckingInterval,
		engine:                       engine,
		strategy:                     strategy,
	}
}

func (rsf *radarSchedulerFactory) BuildScanRunnerFactory(dbPipeline db.Pipeline, externalURL string, variables creds.Variables) radar.ScanRunnerFactory {
	return radar.NewScanRunnerFactory(rsf.pool, rsf.resourceFactory, rsf.resourceConfigFactory, rsf.resourceTypeCheckingInterval, rsf.resourceCheckingInterval, dbPipeline, clock.NewClock(), externalURL, variables, rsf.strategy)
}

func (rsf *radarSchedulerFactory) BuildScheduler() scheduler.BuildScheduler {
	return &scheduler.Scheduler{
		InputMapper: algorithm.NewInputMapper(),
		BuildStarter: scheduler.NewBuildStarter(
			pipeline,
			maxinflight.NewUpdater(pipeline),
			factory.NewBuildFactory(
				pipeline.ID(),
				atc.NewPlanFactory(time.Now().Unix()),
			),
			inputMapper,
			rsf.engine,
		),
	}
}
