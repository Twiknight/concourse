package algorithm

import (
	"errors"
	"fmt"
	"log"
	"strings"
)

// NOTE: we're effectively ignoring check_order here and relying on
// build history - be careful when doing #413 that we don't go
// 'back in time'
//
// QUESTION: is version_md5 worth it? might it be surprising
// that all it takes is (resource name, version identifier)
// to consider something 'passed'?

var ErrLatestVersionNotFound = errors.New("latest version of resource not found")
var ErrVersionNotFound = errors.New("version of resource not found")

type PinnedVersionNotFoundError struct {
	PinnedVersionID int
}

func (e PinnedVersionNotFoundError) Error() string {
	return fmt.Sprintf("pinned version %d not found", e.PinnedVersionID)
}

type version struct {
	ID             int
	VouchedForBy   map[int]bool
	SourceBuildIds []int
	PassedJobIDs   JobSet
	InputName      string
}

func newVersion(id int, passed JobSet, name string) *version {
	return &version{
		ID:             id,
		VouchedForBy:   map[int]bool{},
		SourceBuildIds: []int{},
		PassedJobIDs:   passed,
		InputName:      name,
	}
}

func Resolve(db *VersionsDB, inputConfigs InputConfigs) ([]*version, bool, error) {
	versions := make([]*version, len(inputConfigs))
	for i, input := range inputConfigs {
		versions[i] = &version{
			VouchedForBy:   map[int]bool{},
			SourceBuildIds: []int{},
			PassedJobIDs:   input.Passed,
			InputName:      input.Name,
		}
	}

	unresolvedCandidates := make([]error, len(inputConfigs))

	resolved, err := resolve(0, db, inputConfigs, versions, unresolvedCandidates)
	if err != nil {
		return nil, false, err
	}

	if resolved {
		return versions, true, nil
	}

	return nil, false, nil
}

func resolve(depth int, db *VersionsDB, inputConfigs InputConfigs, candidates []*version, unresolvedCandidates []error) (bool, error) {
	// NOTE: this is probably made most efficient by doing it in order of inputs
	// with jobs that have the broadest output sets, so that we can pin the most
	// at once
	//
	// NOTE 3: maybe also select distinct build outputs so we don't waste time on
	// the same thing (i.e. constantly re-triggered build)
	//
	// NOTE : make sure everything is deterministically ordered

ousdelivery:
	for i, inputConfig := range inputConfigs {
		debug := func(messages ...interface{}) {
			log.Println(
				append(
					[]interface{}{
						strings.Repeat("-", depth) + fmt.Sprintf("[%s]", inputConfig.Name),
					},
					messages...,
				)...,
			)
		}

		if len(inputConfig.Passed) == 0 {
			// coming from recursive call; already set to the latest version
			if candidates[i] != nil {
				continue
			}

			var versionID int
			if inputConfig.PinnedVersionID != 0 {
				// pinned
				exists, err := db.FindVersionOfResource(inputConfig.PinnedVersionID)
				if err != nil {
					return false, err
				}

				if !exists {
					unresolvedCandidates[i] = PinnedVersionNotFoundError{inputConfig.PinnedVersionID}
					continue ousdelivery
				}

				versionID = inputConfig.PinnedVersionID
				debug("setting candidate", i, "to unconstrained version", versionID)
			} else if inputConfig.UseEveryVersion {
				buildID, found, err := db.LatestBuildID(inputConfig.JobID)
				if err != nil {
					return false, err
				}

				if found {
					versionID, found, err = db.NextEveryVersion(buildID, inputConfig.ResourceID)
					if err != nil {
						return false, err
					}

					if !found {
						unresolvedCandidates[i] = ErrVersionNotFound
						continue ousdelivery
					}
				} else {
					versionID, found, err = db.LatestVersionOfResource(inputConfig.ResourceID)
					if err != nil {
						return false, err
					}

					if !found {
						unresolvedCandidates[i] = ErrLatestVersionNotFound
						continue ousdelivery
					}
				}

				debug("setting candidate", i, "to version for version every", versionID)
			} else {
				// there are no passed constraints, so just take the latest version
				var err error
				var found bool
				versionID, found, err = db.LatestVersionOfResource(inputConfig.ResourceID)
				if err != nil {
					return false, nil
				}

				if !found {
					unresolvedCandidates[i] = ErrLatestVersionNotFound
					continue ousdelivery
				}

				debug("setting candidate", i, "to version for latest", versionID)
			}

			candidates[i] = newVersion(versionID, nil, inputConfig.Name)
			continue
		}

		orderedJobs := []int{}
		if len(inputConfig.Passed) != 0 {
			var err error
			orderedJobs, err = db.OrderPassedJobs(inputConfig.JobID, inputConfig.Passed)
			if err != nil {
				return false, err
			}
		}

		for _, jobID := range orderedJobs {
			if candidates[i] != nil {
				debug(i, "has a candidate")

				// coming from recursive call; we've already got a candidate
				if candidates[i].VouchedForBy[jobID] {
					debug("job", jobID, i, "already vouched for", candidates[i].ID)
					// we've already been here; continue to the next job
					continue
				} else {
					debug("job", jobID, i, "has not vouched for", candidates[i].ID)
				}
			} else {
				debug(i, "has no candidate yet")
			}

			// loop over previous output sets, latest first
			var builds []int

			if inputConfig.UseEveryVersion {
				buildID, found, err := db.LatestBuildID(inputConfig.JobID)
				if err != nil {
					return false, err
				}

				if found {
					constraintBuildID, found, err := db.LatestConstraintBuildID(buildID, jobID)
					if err != nil {
						return false, err
					}

					if found {
						builds, err = db.UnusedBuilds(constraintBuildID, jobID)
						if err != nil {
							return false, err
						}
					}
				}
			}

			var err error
			if len(builds) == 0 {
				builds, err = db.SuccessfulBuilds(jobID)
				if err != nil {
					return false, err
				}
			}

			for _, buildID := range builds {
				outputs, err := db.BuildOutputs(buildID)
				if err != nil {
					return false, err
				}

				debug("job", jobID, "trying build", jobID, buildID)

				restore := map[int]*version{}

				var mismatch bool

				// loop over the resource versions that came out of this build set
			outputs:
				for _, output := range outputs {
					debug("build", buildID, "output", output.ResourceID, output.VersionID)

					// try to pin each candidate to the versions from this build
					for c, candidate := range candidates {
						if inputConfigs[c].ResourceID != output.ResourceID {
							// unrelated to this output
							continue
						}

						if !inputConfigs[c].Passed.Contains(jobID) {
							// this candidate is unaffected by the current job
							debug("independent", inputConfigs[c].Passed.String(), jobID)
							continue
						}

						if db.DisabledVersionIDs[output.VersionID] {
							mismatch = true
							break outputs
						}

						if candidate.ID != 0 && candidate.ID != output.VersionID {
							// don't return here! just try the next output set. it's possible
							// we just need to use an older output set.
							debug("mismatch")
							mismatch = true
							break outputs
						}

						// if this doesn't work out, restore it to either nil or the
						// candidate *without* the job vouching for it
						if candidate.ID == 0 {
							// restore[c] = nil
							restore[c] = &version{
								VouchedForBy:   map[int]bool{},
								SourceBuildIds: []int{},
								PassedJobIDs:   candidates[c].PassedJobIDs,
								InputName:      candidates[c].InputName,
							}

							debug("setting candidate", c, "to", output.VersionID)
							candidates[c] = newVersion(output.VersionID, candidates[c].PassedJobIDs, candidates[c].InputName)
						}

						debug("job", jobID, "vouching for", output.ResourceID, "version", output.VersionID)
						candidates[c].VouchedForBy[jobID] = true
						candidates[c].SourceBuildIds = append(candidates[c].SourceBuildIds, buildID)
					}
				}

				// we found a candidate for ourselves and the rest are OK too - recurse
				if candidates[i].ID != 0 && candidates[i].VouchedForBy[jobID] && !mismatch {
					debug("recursing")

					resolved, err := resolve(depth+1, db, inputConfigs, candidates, independentCandidates)
					if err != nil {
						return false, err
					}

					if resolved {
						// we've got a match for the rest of the inputs!
						return true, nil
					}
				}

				debug("restoring")

				for c, version := range restore {
					// either there was a mismatch or resolving didn't work; go on to the
					// next output set
					debug("restoring candidate", c, "to", version)
					candidates[c] = version
				}
			}

			// we've exhausted all the builds and never found a matching input set;
			// time to give up
			return false, nil
		}
	}

	// go to the end of all the inputs - all is well!
	return true, nil
}
