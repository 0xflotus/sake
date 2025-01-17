package dao

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"math"
	"strconv"
	"strings"

	"github.com/alajmo/sake/core"
)

// This is the struct that is added to the Task.Tasks in import_task.go
type TaskCmd struct {
	ID      string
	Name    string
	Desc    string
	WorkDir string
	Shell   string
	RootDir string
	Cmd     string
	Local   bool
	TTY     bool
	Envs    []string
}

// This is the struct that is added to the Task.TaskRefs
type TaskRef struct {
	Name    string
	Desc    string
	Cmd     string
	WorkDir string
	Shell   string
	Task    string
	Local   *bool
	TTY     *bool
	Envs    []string
}

type Task struct {
	ID      string
	Name    string
	Desc    string
	TTY     bool
	Local   bool
	Attach  bool
	WorkDir string
	Shell   string
	Envs    []string
	Cmd     string
	Tasks   []TaskCmd
	Spec    Spec
	Target  Target
	Theme   Theme

	TaskRefs  []TaskRef
	SpecRef   string
	TargetRef string
	ThemeRef  string

	context     string // config path
	contextLine int    // defined at
}

type TaskYAML struct {
	Name    string        `yaml:"name"`
	Desc    string        `yaml:"desc"`
	Local   bool          `yaml:"local"`
	TTY     bool          `yaml:"tty"`
	Attach  bool          `yaml:"attach"`
	WorkDir string        `yaml:"work_dir"`
	Shell   string        `yaml:"shell"`
	Cmd     string        `yaml:"cmd"`
	Task    string        `yaml:"task"`
	Tasks   []TaskRefYAML `yaml:"tasks"`
	Env     yaml.Node     `yaml:"env"`
	Spec    yaml.Node     `yaml:"spec"`
	Target  yaml.Node     `yaml:"target"`
	Theme   yaml.Node     `yaml:"theme"`
}

// This is the struct that will be unmarsheld from YAML
type TaskRefYAML struct {
	Name    string    `yaml:"name"`
	Desc    string    `yaml:"desc"`
	WorkDir string    `yaml:"work_dir"`
	Shell   string    `yaml:"shell"`
	Cmd     string    `yaml:"cmd"`
	Task    string    `yaml:"task"`
	Local   *bool     `yaml:"local"`
	TTY     *bool     `yaml:"tty"`
	Env     yaml.Node `yaml:"env"`
}

func (t Task) GetValue(key string, _ int) string {
	lkey := strings.ToLower(key)
	switch lkey {
	case "name", "task":
		return t.Name
	case "desc", "description":
		return t.Desc
	case "local":
		return strconv.FormatBool(t.Local)
	case "tty":
		return strconv.FormatBool(t.TTY)
	case "attach":
		return strconv.FormatBool(t.Attach)
	case "work_dir":
		return t.WorkDir
	case "shell":
		return t.Shell
	case "spec":
		return t.Spec.Name
	case "target":
		return t.Target.Name
	case "theme":
		return t.Theme.Name
	default:
		return ""
	}
}

func (t *Task) GetDefaultEnvs() []string {
	var defaultEnvs []string
	for _, env := range t.Envs {
		if strings.Contains(env, "SAKE_TASK_") {
			defaultEnvs = append(defaultEnvs, env)
		}
	}

	return defaultEnvs
}

func (t *Task) GetNonDefaultEnvs() []string {
	var envs []string
	for _, env := range t.Envs {
		if !strings.Contains(env, "SAKE_TASK_") {
			envs = append(envs, env)
		}
	}

	return envs
}

func (t *Task) GetContext() string {
	return t.context
}

func (t *Task) GetContextLine() int {
	return t.contextLine
}

// ParseTasksYAML parses the task dictionary and returns it as a list.
// This function also sets task references.
// Valid formats (only one is allowed):
//
//	 cmd: |
//	   echo pong
//
//	 task: ping
//
//	 tasks:
//	   - task: ping
//	   - task: ping
//	   - cmd: echo pong
//
func (c *ConfigYAML) ParseTasksYAML() ([]Task, []ResourceErrors[Task]) {
	var tasks []Task
	count := len(c.Tasks.Content)

	taskErrors := []ResourceErrors[Task]{}
	j := -1
	for i := 0; i < count; i += 2 {
		j += 1
		task := &Task{
			ID:          c.Tasks.Content[i].Value,
			context:     c.Path,
			contextLine: c.Tasks.Content[i].Line,
		}
		re := ResourceErrors[Task]{Resource: task, Errors: []error{}}
		taskErrors = append(taskErrors, re)
		taskYAML := &TaskYAML{}

		if c.Tasks.Content[i+1].Kind == 8 {
			// Shorthand definition:
			// ping: echo 123
			taskYAML.Name = c.Tasks.Content[i].Value
			taskYAML.Cmd = c.Tasks.Content[i+1].Value
		} else {
			// Full definition:
			// ping:
			//   cmd: echo 123
			err := c.Tasks.Content[i+1].Decode(taskYAML)

			if err != nil {
				for _, yerr := range err.(*yaml.TypeError).Errors {
					taskErrors[j].Errors = append(taskErrors[j].Errors, errors.New(yerr))
				}
			}

			// Check that only 1 one 3 is defined (cmd, task, tasks)
			numDefined := 0
			if taskYAML.Cmd != "" {
				numDefined += 1
			}
			if taskYAML.Task != "" {
				numDefined += 1
			}
			if len(taskYAML.Tasks) > 0 {
				numDefined += 1
			}
			if numDefined > 1 {
				taskErrors[j].Errors = append(taskErrors[j].Errors, &core.TaskMultipleDef{Name: c.Tasks.Content[i].Value})
			}

			if numDefined > 1 || err != nil {
				continue
			}
		}

		if taskYAML.Name != "" {
			task.Name = taskYAML.Name
		} else {
			task.Name = c.Tasks.Content[i].Value
		}
		task.Desc = taskYAML.Desc
		task.TTY = taskYAML.TTY
		task.Local = taskYAML.Local
		task.WorkDir = taskYAML.WorkDir
		task.Shell = taskYAML.Shell
		task.Attach = taskYAML.Attach

		defaultEnvs := []string{
			fmt.Sprintf("SAKE_TASK_ID=%s", task.ID),
			fmt.Sprintf("SAKE_TASK_NAME=%s", taskYAML.Name),
			fmt.Sprintf("SAKE_TASK_DESC=%s", taskYAML.Desc),
		}

		if taskYAML.Local {
			defaultEnvs = append(defaultEnvs, fmt.Sprintf("SAKE_TASK_LOCAL=%t", taskYAML.Local))
		}

		task.Envs = append(task.Envs, defaultEnvs...)

		if !IsNullNode(taskYAML.Env) {
			err := CheckIsMappingNode(taskYAML.Env)
			if err != nil {
				taskErrors[j].Errors = append(taskErrors[j].Errors, err)
			} else {
				task.Envs = append(task.Envs, ParseNodeEnv(taskYAML.Env)...)
			}
		}

		task.Tasks = []TaskCmd{}
		task.TaskRefs = []TaskRef{}

		// Spec
		if len(taskYAML.Spec.Content) > 0 {
			// Spec value
			spec := &Spec{}
			err := taskYAML.Spec.Decode(spec)
			if err != nil {
				for _, yerr := range err.(*yaml.TypeError).Errors {
					taskErrors[j].Errors = append(taskErrors[j].Errors, errors.New(yerr))
				}
			} else {
				task.Spec = *spec
			}
		} else if taskYAML.Spec.Value != "" {
			// Spec reference
			task.SpecRef = taskYAML.Spec.Value
		} else {
			task.SpecRef = DEFAULT_SPEC.Name
		}

		// Target
		if len(taskYAML.Target.Content) > 0 {
			// Target value
			target := &Target{}
			err := taskYAML.Target.Decode(target)
			if err != nil {
				for _, yerr := range err.(*yaml.TypeError).Errors {
					taskErrors[j].Errors = append(taskErrors[j].Errors, errors.New(yerr))
				}
			} else {
				task.Target = *target
			}
		} else if taskYAML.Target.Value != "" {
			// Target reference
			task.TargetRef = taskYAML.Target.Value
		} else {
			task.TargetRef = DEFAULT_TARGET.Name
		}

		// Theme
		if len(taskYAML.Theme.Content) > 0 {
			// Theme value
			theme := &Theme{}
			err := taskYAML.Theme.Decode(theme)
			if err != nil {
				for _, yerr := range err.(*yaml.TypeError).Errors {
					taskErrors[j].Errors = append(taskErrors[j].Errors, errors.New(yerr))
				}
			} else {
				task.Theme = *theme
			}
		} else if taskYAML.Theme.Value != "" {
			// Theme reference
			task.ThemeRef = taskYAML.Theme.Value
		} else {
			task.ThemeRef = DEFAULT_THEME.Name
		}

		// Set task cmd/reference
		if taskYAML.Task != "" {
			// Task Reference
			tr := TaskRef{
				Task: taskYAML.Task,
			}

			task.TaskRefs = append(task.TaskRefs, tr)
		} else if len(taskYAML.Tasks) > 0 {
			// Tasks References
			for k := range taskYAML.Tasks {
				tr := TaskRef{
					Name:    taskYAML.Tasks[k].Name,
					Desc:    taskYAML.Tasks[k].Desc,
					WorkDir: taskYAML.Tasks[k].WorkDir,
					Shell:   taskYAML.Tasks[k].Shell,
					Local:   taskYAML.Tasks[k].Local,
					TTY:     taskYAML.Tasks[k].TTY,
					Envs:    ParseNodeEnv(taskYAML.Tasks[k].Env),
				}

				// Check that only cmd or task is defined
				if taskYAML.Tasks[k].Cmd != "" && taskYAML.Tasks[k].Task != "" {
					taskErrors[j].Errors = append(taskErrors[j].Errors, &core.TaskRefMultipleDef{Name: c.Tasks.Content[i].Value})
					continue
				} else if taskYAML.Tasks[k].Cmd != "" {
					tr.Cmd = taskYAML.Tasks[k].Cmd
				} else if taskYAML.Tasks[k].Task != "" {
					tr.Task = taskYAML.Tasks[k].Task
				} else {
					taskErrors[j].Errors = append(taskErrors[j].Errors, &core.NoTaskRefDefined{Name: c.Tasks.Content[i].Value})
					continue
				}

				task.TaskRefs = append(task.TaskRefs, tr)
			}
		} else if taskYAML.Cmd != "" {
			// Command
			task.Cmd = taskYAML.Cmd
		}

		tasks = append(tasks, *task)
	}

	return tasks, taskErrors
}

func ParseTaskEnv(cmdEnv []string, userEnv []string, parentEnv []string, configEnv []string) ([]string, error) {
	cmdEnv, err := EvaluateEnv(cmdEnv)
	if err != nil {
		return []string{}, err
	}

	pEnv, err := EvaluateEnv(parentEnv)
	if err != nil {
		return []string{}, err
	}

	envs := MergeEnvs(userEnv, cmdEnv, pEnv, configEnv)

	return envs, nil
}

func (c *Config) GetTaskServers(task *Task, runFlags *core.RunFlags, setRunFlags *core.SetRunFlags) ([]Server, error) {
	var servers []Server
	var err error
	// If any runtime target flags are used, disregard config specified task targets
	if len(runFlags.Servers) > 0 || len(runFlags.Tags) > 0 || runFlags.Regex != "" || setRunFlags.All || setRunFlags.Invert {
		servers, err = c.FilterServers(runFlags.All, runFlags.Servers, runFlags.Tags, runFlags.Regex, runFlags.Invert)
	} else {
		servers, err = c.FilterServers(task.Target.All, task.Target.Servers, task.Target.Tags, task.Target.Regex, runFlags.Invert)
	}

	if err != nil {
		return []Server{}, err
	}

	var limit uint32
	if runFlags.Limit > 0 {
		limit = runFlags.Limit
	} else if task.Target.Limit > 0 {
		limit = task.Target.Limit
	}

	var limitp uint8
	if runFlags.LimitP > 0 {
		limitp = runFlags.LimitP
	} else if task.Target.LimitP > 0 {
		limitp = task.Target.LimitP
	}

	if limit > 0 {
		if limit <= uint32(len(servers)) {
			return servers[0:limit], nil
		} else {
			return []Server{}, &core.InvalidLimit{Max: len(servers), Limit: limit}
		}
	} else if limitp > 0 {
		if limitp <= 100 {
			tot := float64(len(servers))
			percentage := float64(limitp) / float64(100)
			limit := math.Floor(percentage * tot)
			return servers[0:int(limit)], nil
		} else {
			return []Server{}, &core.InvalidPercentInput{}
		}
	}

	return servers, nil
}

func (c *Config) GetTasksByIDs(ids []string) ([]Task, error) {
	if len(ids) == 0 {
		return c.Tasks, nil
	}

	foundTasks := make(map[string]bool)
	for _, t := range ids {
		foundTasks[t] = false
	}

	var filteredTasks []Task
	for _, id := range ids {
		if foundTasks[id] {
			continue
		}

		for _, task := range c.Tasks {
			if id == task.ID {
				foundTasks[task.ID] = true
				filteredTasks = append(filteredTasks, task)
			}
		}
	}

	nonExistingTasks := []string{}
	for k, v := range foundTasks {
		if !v {
			nonExistingTasks = append(nonExistingTasks, k)
		}
	}

	if len(nonExistingTasks) > 0 {
		return []Task{}, &core.TaskNotFound{IDs: nonExistingTasks}
	}

	return filteredTasks, nil
}

func (c *Config) GetTaskNames() []string {
	taskNames := []string{}
	for _, task := range c.Tasks {
		taskNames = append(taskNames, task.Name)
	}

	return taskNames
}

func (c *Config) GetTaskIDAndDesc() []string {
	taskNames := []string{}
	for _, task := range c.Tasks {
		if task.Desc != "" {
			taskNames = append(taskNames, fmt.Sprintf("%s\t%s", task.ID, task.Desc))
		} else if task.ID != task.Name {
			taskNames = append(taskNames, fmt.Sprintf("%s\t%s", task.ID, task.Name))
		} else {
			taskNames = append(taskNames, task.ID)
		}
	}

	return taskNames
}

func (c *Config) GetTask(id string) (*Task, error) {
	for _, task := range c.Tasks {
		if id == task.ID {
			return &task, nil
		}
	}

	return nil, &core.TaskNotFound{IDs: []string{id}}
}
