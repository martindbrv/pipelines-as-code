package resolve

import (
	"fmt"
	"os"
	"strings"
	"testing"

	apipac "github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/keys"
	"github.com/openshift-pipelines/pipelines-as-code/pkg/matcher"
	"github.com/openshift-pipelines/pipelines-as-code/pkg/params"
	"github.com/openshift-pipelines/pipelines-as-code/pkg/params/clients"
	httptesthelper "github.com/openshift-pipelines/pipelines-as-code/pkg/test/http"
	testprovider "github.com/openshift-pipelines/pipelines-as-code/pkg/test/provider"
	ttkn "github.com/openshift-pipelines/pipelines-as-code/pkg/test/tekton"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"go.uber.org/zap"
	zapobserver "go.uber.org/zap/zaptest/observer"
	"gotest.tools/v3/assert"
	rtesting "knative.dev/pkg/reconciler/testing"
	"sigs.k8s.io/yaml"
)

func TestRemote(t *testing.T) {
	randomPipelineRunName := "pipelinerun-abc"
	remotePipelineName := "remote-pipeline"
	remotePipelineURL := "http://remote/" + remotePipelineName

	taskFromPipelineRunName := "task-from-pipelinerun"
	taskFromPipelineRunURL := "http://remote/" + taskFromPipelineRunName

	remoteTaskName := "remote-task"
	remoteTaskURL := "http://remote/" + remoteTaskName
	taskFromPipelineSpec := tektonv1.TaskSpec{
		Steps: []tektonv1.Step{
			{
				Name:    "step1",
				Image:   "scratch",
				Command: []string{"true"},
			},
		},
	}
	taskFromPipelineRunSpec := tektonv1.TaskSpec{
		Steps: []tektonv1.Step{
			{
				Name:    "frompipelinerun",
				Image:   "scratch",
				Command: []string{"false"},
			},
		},
	}
	pipelineTaskEmbedded := []tektonv1.PipelineTask{
		{
			Name: "embedded",
			TaskSpec: &tektonv1.EmbeddedTask{
				TaskSpec: taskFromPipelineSpec,
			},
		},
	}
	pipelinewithTaskEmbedded := ttkn.MakePipeline(remotePipelineName, pipelineTaskEmbedded, nil)
	pipelinewithTaskEmbeddedB, err := yaml.Marshal(pipelinewithTaskEmbedded)
	assert.NilError(t, err)

	pipelineTaskRef := []tektonv1.PipelineTask{
		{
			Name: remoteTaskName,
			TaskRef: &tektonv1.TaskRef{
				Name: remoteTaskName,
			},
		},
	}
	pipelinewithTaskRef := ttkn.MakePipeline(remotePipelineName, pipelineTaskRef, map[string]string{
		apipac.Task: remoteTaskURL,
	})
	pipelinewithTaskRefYamlB, err := yaml.Marshal(pipelinewithTaskRef)
	assert.NilError(t, err)

	pipelineRelativeTaskRef := []tektonv1.PipelineTask{
		{
			Name: remoteTaskName + "-a",
			TaskRef: &tektonv1.TaskRef{
				Name: remoteTaskName + "-a",
			},
		},
		{
			Name: remoteTaskName + "-b",
			TaskRef: &tektonv1.TaskRef{
				Name: remoteTaskName + "-b",
			},
		},
		{
			Name: remoteTaskName + "-c",
			TaskRef: &tektonv1.TaskRef{
				Name: remoteTaskName + "-c",
			},
		},
		{
			Name: remoteTaskName + "-d",
			TaskRef: &tektonv1.TaskRef{
				Name: remoteTaskName + "-d",
			},
		},
	}
	pipelineWithRelativeTaskRef := ttkn.MakePipeline(remotePipelineName, pipelineRelativeTaskRef[:3], map[string]string{
		apipac.Task:        "./" + remoteTaskName + "-a",
		apipac.Task + "-1": "../" + remoteTaskName + "-b",
		apipac.Task + "-2": "../../../../" + remoteTaskName + "-c",
	})

	pipelineWithRelativeTaskRefYamlB, err := yaml.Marshal(pipelineWithRelativeTaskRef)
	assert.NilError(t, err)

	pipelineWithRelativeTaskRef1 := ttkn.MakePipeline(remotePipelineName, pipelineRelativeTaskRef[1:], map[string]string{
		apipac.Task:        remoteTaskName + "-b",
		apipac.Task + "-1": "utils/" + remoteTaskName + "-c",
		apipac.Task + "-2": " " + remoteTaskName + "-d",
	})

	pipelineWithRelativeTaskRefYamlB1, err := yaml.Marshal(pipelineWithRelativeTaskRef1)
	assert.NilError(t, err)

	singleRelativeTaskBa, err := ttkn.MakeTaskB(remoteTaskName+"-a", taskFromPipelineSpec)
	assert.NilError(t, err)

	singleRelativeTaskBb, err := ttkn.MakeTaskB(remoteTaskName+"-b", taskFromPipelineSpec)
	assert.NilError(t, err)

	singleRelativeTaskBc, err := ttkn.MakeTaskB(remoteTaskName+"-c", taskFromPipelineSpec)
	assert.NilError(t, err)

	singleRelativeTaskBd, err := ttkn.MakeTaskB(remoteTaskName+"-d", taskFromPipelineSpec)
	assert.NilError(t, err)

	singleTask := ttkn.MakeTask(remoteTaskName, taskFromPipelineSpec)
	singleTaskB, err := yaml.Marshal(singleTask)
	assert.NilError(t, err)

	taskFromPipelineRun := ttkn.MakeTask(remoteTaskName, taskFromPipelineRunSpec)
	taskFromPipelineRunB, err := yaml.Marshal(taskFromPipelineRun)
	assert.NilError(t, err)

	tests := []struct {
		name                 string
		pipelineruns         []*tektonv1.PipelineRun
		tasks                []*tektonv1.Task
		pipelines            []*tektonv1.Pipeline
		wantErrSnippet       string
		remoteURLS           map[string]map[string]string
		expectedLogsSnippets []string
		expectedTaskSpec     tektonv1.TaskSpec
		expectedPipelineRun  []string
		noPipelineRun        bool
	}{
		{
			name: "remote pipeline with remote task from pipeline",
			pipelineruns: []*tektonv1.PipelineRun{
				ttkn.MakePR(randomPipelineRunName, map[string]string{
					apipac.Pipeline: remotePipelineURL,
				},
					tektonv1.PipelineRunSpec{
						PipelineRef: &tektonv1.PipelineRef{
							Name: "remote-pipeline",
						},
					},
				),
			},
			remoteURLS: map[string]map[string]string{
				"http://remote/embedpipeline": {
					"body": string(pipelinewithTaskEmbeddedB),
					"code": "200",
				},
				remotePipelineURL: {
					"body": string(pipelinewithTaskRefYamlB),
					"code": "200",
				},
				remoteTaskURL: {
					"body": string(singleTaskB),
					"code": "200",
				},
			},
			expectedTaskSpec: taskFromPipelineSpec,
			expectedLogsSnippets: []string{
				fmt.Sprintf("successfully fetched %s from remote https url", remotePipelineURL),
				fmt.Sprintf("successfully fetched %s from remote https url", remoteTaskURL),
			},
			expectedPipelineRun: []string{"remote-pipeline-with-remote-task-from-pipeline.yaml"},
		},
		{
			name: "remote pipelines with relative tasks",
			pipelineruns: []*tektonv1.PipelineRun{
				ttkn.MakePR(randomPipelineRunName, map[string]string{
					apipac.Pipeline: remotePipelineURL,
				},
					tektonv1.PipelineRunSpec{
						PipelineRef: &tektonv1.PipelineRef{
							Name: remotePipelineName,
						},
					},
				),
				ttkn.MakePR(randomPipelineRunName, map[string]string{
					apipac.Pipeline: remotePipelineURL + "-1",
				},
					tektonv1.PipelineRunSpec{
						PipelineRef: &tektonv1.PipelineRef{
							Name: remotePipelineName,
						},
					},
				),
			},
			remoteURLS: map[string]map[string]string{
				remotePipelineURL: {
					"body": string(pipelineWithRelativeTaskRefYamlB),
					"code": "200",
				},
				remotePipelineURL + "-1": {
					"body": string(pipelineWithRelativeTaskRefYamlB1),
					"code": "200",
				},
				remoteTaskURL + "-a": {
					"body": string(singleRelativeTaskBa),
					"code": "200",
				},
				remoteTaskURL + "-b": {
					"body": string(singleRelativeTaskBb),
					"code": "200",
				},
				remoteTaskURL + "-c": {
					"body": string(singleRelativeTaskBc),
					"code": "200",
				},
				"http://remote/utils/remote-task-c": {
					"body": string(singleRelativeTaskBc),
					"code": "200",
				},
				remoteTaskURL + "-d": {
					"body": string(singleRelativeTaskBd),
					"code": "200",
				},
			},
			expectedTaskSpec:     taskFromPipelineSpec,
			expectedLogsSnippets: []string{},
			expectedPipelineRun: []string{
				"remote-pipeline-with-relative-tasks.yaml",
				"remote-pipeline-with-relative-tasks-1.yaml",
			},
		},
		{
			name: "remote pipeline with remote task in pipeline overridden from pipelinerun",
			pipelineruns: []*tektonv1.PipelineRun{
				ttkn.MakePR(randomPipelineRunName, map[string]string{
					apipac.Pipeline: remotePipelineURL,
					apipac.Task:     taskFromPipelineRunURL,
				},
					tektonv1.PipelineRunSpec{
						PipelineRef: &tektonv1.PipelineRef{
							Name: "remote-pipeline",
						},
					},
				),
			},
			expectedTaskSpec: taskFromPipelineRunSpec,
			remoteURLS: map[string]map[string]string{
				remotePipelineURL: {
					"body": string(pipelinewithTaskRefYamlB),
					"code": "200",
				},
				remoteTaskURL: {
					"body": string(singleTaskB),
					"code": "200",
				},
				taskFromPipelineRunURL: {
					"body": string(taskFromPipelineRunB),
					"code": "200",
				},
			},
			expectedLogsSnippets: []string{
				fmt.Sprintf("successfully fetched %s from remote https url", remotePipelineURL),
				fmt.Sprintf("successfully fetched %s from remote https url", taskFromPipelineRunURL),
			},
			expectedPipelineRun: []string{"remote-pipeline-with-remote-task-from-pipelinerun.yaml"},
		},
		{
			name: "remote pipelinerun no annotations",
			pipelineruns: []*tektonv1.PipelineRun{
				ttkn.MakePR(randomPipelineRunName, map[string]string{},
					tektonv1.PipelineRunSpec{
						PipelineRef: &tektonv1.PipelineRef{
							Name: "remote-pipeline",
						},
					},
				),
			},
			noPipelineRun: true,
		},
		{
			name:           "error/remote pipelinerun is 404",
			wantErrSnippet: "error getting remote pipeline \"" + remotePipelineURL + "\"",
			pipelineruns: []*tektonv1.PipelineRun{
				ttkn.MakePR(randomPipelineRunName, map[string]string{
					apipac.Pipeline: remotePipelineURL,
				},
					tektonv1.PipelineRunSpec{
						PipelineRef: &tektonv1.PipelineRef{
							Name: "remote-pipeline",
						},
					},
				),
			},
		},
		{
			name: "skip fetching multiple tasks of the same name from pipelinerun annotations and pipeline annotation",
			pipelineruns: []*tektonv1.PipelineRun{
				ttkn.MakePR(randomPipelineRunName, map[string]string{
					apipac.Pipeline:    remotePipelineURL,
					apipac.Task:        remoteTaskURL,
					apipac.Task + "-1": remoteTaskURL,
				},
					tektonv1.PipelineRunSpec{
						PipelineRef: &tektonv1.PipelineRef{
							Name: "remote-pipeline",
						},
					},
				),
			},
			remoteURLS: map[string]map[string]string{
				remotePipelineURL: {
					"body": string(pipelinewithTaskRefYamlB),
					"code": "200",
				},
				remoteTaskURL: {
					"body": string(singleTaskB),
					"code": "200",
				},
			},
			expectedTaskSpec: taskFromPipelineSpec,
			expectedLogsSnippets: []string{
				fmt.Sprintf("successfully fetched %s from remote https url", remotePipelineURL),
				fmt.Sprintf("successfully fetched %s from remote https url", remoteTaskURL),
			},
			expectedPipelineRun: []string{"skip-fetching-multiple-tasks-of-the-same-name-from-pipelinerun-annotations-and-pipeline-annotation.yaml"},
		},
		{
			name: "skip fetching multiple tasks of the same name from pipelinerun annotations and tektondir",
			pipelineruns: []*tektonv1.PipelineRun{
				ttkn.MakePR(randomPipelineRunName, map[string]string{
					apipac.Pipeline: remotePipelineURL,
					apipac.Task:     remoteTaskURL,
				},
					tektonv1.PipelineRunSpec{
						PipelineRef: &tektonv1.PipelineRef{
							Name: "remote-pipeline",
						},
					},
				),
			},
			tasks: []*tektonv1.Task{
				singleTask,
			},
			remoteURLS: map[string]map[string]string{
				remotePipelineURL: {
					"body": string(pipelinewithTaskRefYamlB),
					"code": "200",
				},
				remoteTaskURL: {
					"body": string(singleTaskB),
					"code": "200",
				},
			},
			expectedTaskSpec: taskFromPipelineSpec,
			expectedLogsSnippets: []string{
				fmt.Sprintf("successfully fetched %s from remote https url", remotePipelineURL),
				fmt.Sprintf("successfully fetched %s from remote https url", remoteTaskURL),
				fmt.Sprintf("skipping remote task %s as already fetched task %s for pipelinerun %s", remoteTaskURL, remoteTaskName, randomPipelineRunName),
				fmt.Sprintf("overriding task %s coming from .tekton directory by an annotation task for pipelinerun %s", remoteTaskName, randomPipelineRunName),
			},
			expectedPipelineRun: []string{"skip-fetching-multiple-tasks-of-the-same-name-from-pipelinerun-annotations-and-tektondir.yaml"},
		},
		{
			name: "skip fetching multiple pipelines of the same name from pipelinerun annotations and tektondir",
			pipelineruns: []*tektonv1.PipelineRun{
				ttkn.MakePR(randomPipelineRunName, map[string]string{
					apipac.Pipeline: remotePipelineURL,
					apipac.Task:     remoteTaskURL,
				},
					tektonv1.PipelineRunSpec{
						PipelineRef: &tektonv1.PipelineRef{
							Name: "remote-pipeline",
						},
					},
				),
			},
			pipelines: []*tektonv1.Pipeline{
				pipelinewithTaskEmbedded,
			},
			remoteURLS: map[string]map[string]string{
				remotePipelineURL: {
					"body": string(pipelinewithTaskRefYamlB),
					"code": "200",
				},
				remoteTaskURL: {
					"body": string(singleTaskB),
					"code": "200",
				},
			},
			expectedTaskSpec: taskFromPipelineSpec,
			expectedLogsSnippets: []string{
				fmt.Sprintf("successfully fetched %s from remote https url", remotePipelineURL),
				fmt.Sprintf("successfully fetched %s from remote https url", remoteTaskURL),
				fmt.Sprintf("skipping remote task %s as already fetched task %s for pipelinerun %s", remoteTaskURL, remoteTaskName, randomPipelineRunName),
			},
			expectedPipelineRun: []string{"skip-fetching-multiple-pipelines-of-the-same-name-from-pipelinerun-annotations-and-tektondir.yaml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, log := zapobserver.New(zap.InfoLevel)
			logger := zap.New(observer).Sugar()
			ctx, _ := rtesting.SetupFakeContext(t)
			tktype := TektonTypes{
				Pipelines:    tt.pipelines,
				Tasks:        tt.tasks,
				PipelineRuns: tt.pipelineruns,
			}

			tprovider := &testprovider.TestProviderImp{}
			httpTestClient := httptesthelper.MakeHTTPTestClient(tt.remoteURLS)
			rt := &matcher.RemoteTasks{
				ProviderInterface: tprovider,
				Logger:            logger,
				Run: &params.Run{
					Clients: clients.Clients{
						HTTP: *httpTestClient,
					},
				},
			}
			ret, err := resolveRemoteResources(ctx, rt, tktype, &Opts{RemoteTasks: true, GenerateName: true})
			if tt.wantErrSnippet != "" {
				assert.ErrorContains(t, err, tt.wantErrSnippet)
				return
			}
			assert.NilError(t, err)

			for k, snippet := range tt.expectedLogsSnippets {
				logmsg := log.AllUntimed()[k].Message
				assert.Assert(t, strings.Contains(logmsg, snippet), "\n on index: %d\n we want: %s\n we  got: %s", k, snippet, logmsg)
			}

			if tt.noPipelineRun {
				assert.Assert(t, len(ret) == 0, "not expecting any pipelinerun")
				return
			}
			for i, pr := range ret {
				if len(tt.expectedPipelineRun) < len(ret) {
					assert.NilError(t, fmt.Errorf("insufficient amount of expectedPipelineRuns was provided, got %d but want %d; or set noPipelineRun to true",
						len(tt.expectedPipelineRun), len(ret)))
				}
				expectedData, err := os.ReadFile("testdata/" + tt.expectedPipelineRun[i])
				assert.NilError(t, err)
				pipelineRun := &tektonv1.PipelineRun{}
				err = yaml.Unmarshal(expectedData, pipelineRun)
				assert.NilError(t, err)
				assert.DeepEqual(t, pipelineRun, pr)
			}
		})
	}
}
