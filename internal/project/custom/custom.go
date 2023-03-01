// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package custom provides blocks defined in the config manually.
package custom

import (
	"fmt"

	"github.com/siderolabs/kres/internal/dag"
	"github.com/siderolabs/kres/internal/output/dockerfile"
	dockerstep "github.com/siderolabs/kres/internal/output/dockerfile/step"
	"github.com/siderolabs/kres/internal/output/drone"
	"github.com/siderolabs/kres/internal/output/makefile"
	"github.com/siderolabs/kres/internal/project/meta"
)

// Step is defined in the config manually.
//
//nolint:govet
type Step struct {
	dag.BaseNode

	meta *meta.Options

	Docker struct {
		Stages []struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
			From        string `yaml:"from"`
			Steps       []struct {
				Script *struct {
					Command string   `yaml:"command"`
					Cache   []string `yaml:"cache"`
				} `yaml:"script"`
				Copy *struct {
					From string `yaml:"from"`
					Src  string `yaml:"src"`
					Dst  string `yaml:"dst"`
				} `yaml:"copy"`
				Arg string `yaml:"arg"`
			} `yaml:"steps"`
		} `yaml:"stages"`

		Enabled bool `yaml:"enabled"`
	} `yaml:"docker"`

	Makefile struct { //nolint:govet
		Enabled bool     `yaml:"enabled"`
		Phony   bool     `yaml:"phony"`
		Depends []string `yaml:"depends"`
		Script  []string `yaml:"script"`

		Variables []struct {
			Name         string `yaml:"name"`
			DefaultValue string `yaml:"defaultValue"`
		} `yaml:"variables"`
	} `yaml:"makefile"`

	Drone struct {
		Enabled     bool              `yaml:"enabled"`
		Privileged  bool              `yaml:"privileged"`
		Environment map[string]string `yaml:"environment"`
		Requests    *struct {
			CPUCores  int `yaml:"cpuCores"`
			MemoryGiB int `yaml:"memoryGiB"`
		} `yaml:"requests"`
		Volumes []struct {
			Name      string `yaml:"name"`
			MountPath string `yaml:"mountPath"`
		} `yaml:"volumes"`
		Pipelines []struct {
			Name                string            `yaml:"name"`
			Triggers            []string          `yaml:"triggers"`
			Crons               []string          `yaml:"crons"`
			EnvironmentOverride map[string]string `yaml:"environmentOverride"`
		}
	} `yaml:"drone"`
}

// NewStep initializes Step.
func NewStep(meta *meta.Options, name string) *Step {
	return &Step{
		BaseNode: dag.NewBaseNode(name),
		meta:     meta,
	}
}

// CompileDockerfile implements dockerfile.Compiler.
func (step *Step) CompileDockerfile(output *dockerfile.Output) error {
	if !step.Docker.Enabled {
		return nil
	}

	for _, stage := range step.Docker.Stages {
		s := output.Stage(stage.Name).Description(stage.Description)

		if stage.From != "" {
			s.From(stage.From)
		} else {
			s.From("scratch")
		}

		for _, stageStep := range stage.Steps {
			switch {
			case stageStep.Arg != "":
				s.Step(dockerstep.Arg(stageStep.Arg))
			case stageStep.Script != nil:
				script := dockerstep.Script(stageStep.Script.Command)
				for _, cache := range stageStep.Script.Cache {
					script.MountCache(cache)
				}

				s.Step(script)
			case stageStep.Copy != nil:
				copyStep := dockerstep.Copy(stageStep.Copy.Src, stageStep.Copy.Dst)
				if stageStep.Copy.From != "" {
					copyStep.From(stageStep.Copy.From)
				}

				s.Step(copyStep)
			}
		}
	}

	return nil
}

// CompileDrone implements drone.Compiler.
func (step *Step) CompileDrone(output *drone.Output) error {
	if !step.Drone.Enabled {
		return nil
	}

	droneMatches := func(node dag.Node) bool {
		if !dag.Implements[drone.Compiler]()(node) {
			return false
		}

		if nodeStep, ok := node.(*Step); ok {
			return nodeStep.Drone.Enabled
		}

		return true
	}

	baseDroneStep := func() *drone.Step {
		droneStep := drone.MakeStep(step.Name())

		if step.Drone.Privileged {
			droneStep.Privileged()
		}

		if step.Drone.Requests != nil {
			droneStep.ResourceRequests(step.Drone.Requests.CPUCores, step.Drone.Requests.MemoryGiB)
		}

		for k, v := range step.Drone.Environment {
			droneStep.Environment(k, v)
		}

		for _, volume := range step.Drone.Volumes {
			droneStep.EmptyDirVolume(volume.Name, volume.MountPath)
		}

		return droneStep
	}

	output.Step(baseDroneStep().
		DependsOn(dag.GatherMatchingInputNames(step, droneMatches)...))

	for _, customPipeline := range step.Drone.Pipelines {
		pipeline := output.Pipeline(customPipeline.Name, customPipeline.Triggers, customPipeline.Crons)

		type baseDroneStepper interface {
			BuildBaseDroneSteps(output drone.StepService)
		}

		baseStepNames := []string{}

		for _, baseInput := range dag.GatherMatchingInputsRecursive(step, dag.Implements[baseDroneStepper]()) {
			baseInput.(baseDroneStepper).BuildBaseDroneSteps(pipeline) //nolint:forcetypeassert // type is checked in GatherMatchingInputsRecursive

			baseStepNames = append(baseStepNames, baseInput.Name())
		}

		pipeline.Step(
			drone.CustomStep("load-artifacts",
				"s3cmd --host=rook-ceph-rgw-ci-store.rook-ceph.svc --host-bucket=rook-ceph-rgw-ci-store.rook-ceph.svc --no-ssl --stats sync s3://${CI_COMMIT_SHA}${DRONE_TAG//./-} .",
			).
				DependsOn(baseStepNames...).
				EnvironmentFromSecret("AWS_ACCESS_KEY_ID", "rook_access_key_id").
				EnvironmentFromSecret("AWS_SECRET_ACCESS_KEY", "rook_secret_access_key"),
		)

		droneStep := baseDroneStep()

		for k, v := range customPipeline.EnvironmentOverride {
			droneStep.Environment(k, v)
		}

		pipeline.Step(droneStep.DependsOn("load-artifacts"))
	}

	if len(step.Drone.Pipelines) > 0 {
		// add a "save artifacts" step to the default pipeline
		output.Step(
			drone.CustomStep("save-artifacts",
				"s3cmd --host=rook-ceph-rgw-ci-store.rook-ceph.svc --host-bucket=rook-ceph-rgw-ci-store.rook-ceph.svc --no-ssl mb s3://${CI_COMMIT_SHA}${DRONE_TAG//./-}",
				"s3cmd --host=rook-ceph-rgw-ci-store.rook-ceph.svc --host-bucket=rook-ceph-rgw-ci-store.rook-ceph.svc --no-ssl expire s3://${CI_COMMIT_SHA}${DRONE_TAG//./-} --expiry-days=3",
				fmt.Sprintf(
					"s3cmd --host=rook-ceph-rgw-ci-store.rook-ceph.svc --host-bucket=rook-ceph-rgw-ci-store.rook-ceph.svc --no-ssl --stats sync %s s3://${CI_COMMIT_SHA}${DRONE_TAG//./-}",
					step.meta.ArtifactsPath,
				),
			).
				DependsOn(step.Name()).
				EnvironmentFromSecret("AWS_ACCESS_KEY_ID", "rook_access_key_id").
				EnvironmentFromSecret("AWS_SECRET_ACCESS_KEY", "rook_secret_access_key"),
		)
	}

	return nil
}

// DroneEnabled implements drone.CustomCompiler.
func (step *Step) DroneEnabled() bool {
	return step.Drone.Enabled
}

// CompileMakefile implements makefile.Compiler.
func (step *Step) CompileMakefile(output *makefile.Output) error {
	if !step.Makefile.Enabled {
		return nil
	}

	for _, variable := range step.Makefile.Variables {
		output.VariableGroup(makefile.VariableGroupExtra).
			Variable(makefile.OverridableVariable(variable.Name, variable.DefaultValue))
	}

	target := output.Target(step.Name()).
		Depends(step.Makefile.Depends...).
		Script(step.Makefile.Script...)

	if step.Makefile.Phony {
		target.Phony()
	}

	return nil
}
