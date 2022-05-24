package flow

import (
	"net/http"

	klog "k8s.io/klog/v2"

	"github.com/jasony62/tms-go-apihub/hub"
	"github.com/jasony62/tms-go-apihub/task"
	"github.com/jasony62/tms-go-apihub/unit"
	"github.com/jasony62/tms-go-apihub/util"
)

type concurrentFlowIn struct {
	taskDef *hub.TaskDef
}
type concurrentFlowOut struct {
	taskDef *hub.TaskDef
	result  interface{}
}

func copyFlowStack(src *hub.Stack) *hub.Stack {
	stepResult := make(map[string]interface{})
	for k, v := range src.StepResult {
		stepResult[k] = v
	}

	//avoid vars race conditions
	return &hub.Stack{
		GinContext: src.GinContext,
		RootName:   src.RootName,
		StepResult: stepResult,
	}
}

func fillOrigin(stack *hub.Stack, parameters *[]hub.OriginDefParam, src int) {
	var value string
	origin := stack.StepResult[hub.OriginName].(map[string]interface{})

	for _, parameter := range *parameters {
		if (src == hub.ORIGIN_SRC_API) || (src == hub.ORIGIN_SRC_RESPONSE && parameter.In == "query") {
			if len(parameter.Value) > 0 {
				value = parameter.Value
			} else {
				value = unit.GetParameterValue(stack, nil, parameter.From)
			}

			oldValue, isOk := origin[parameter.Name]
			if isOk {
				klog.Infoln("replace ", parameter.Name, " from ", oldValue, " to ", value)
			}
			origin[parameter.Name] = value
		}
	}
}

func handleOneTask(stack *hub.Stack, taskDef *hub.TaskDef) (result interface{}, ret int) {
	if len(taskDef.Command) > 0 {
		return task.Run(stack, taskDef)
	} else if taskDef.Response != nil && taskDef.Response.From != nil {
		// 处理响应结果
		klog.Infoln("handleOneApi响应格式", step.Response.Type)

		var rules interface{}
		if step.Response.Type == hub.RESP_TYPE_TMPL {
			if step.Response.Parameters != nil && len(*step.Response.Parameters) > 0 {
				// 根据flow的定义改写origin
				fillOrigin(stack, step.Response.Parameters, hub.ORIGIN_SRC_RESPONSE)
				fillVars(stack, step.Response.PrivateName, step.Response.Parameters)
			}

			strhtml := hub.DefaultApp.TemplateMap[step.Response.From.Content]
			result = util.Json2Html(stack.StepResult, strhtml)
			klog.Infoln("get final template result：", result)
		} else {
			if step.Response.Type == hub.RESP_TYPE_JSON {
				rules = step.Response.From.Json
			} else if step.Response.Type == hub.RESP_TYPE_HTML {
				rules = step.Response.From.Content
			}
			result = util.Json2Json(stack.StepResult, rules)
			if result == nil {
				klog.Infoln("get final result failed：", rules, "\r\n", stack.StepResult, "\r\n", result)
			} else {
				klog.Infoln("get final result：", result)
			}
		}
	}
	return result, 200
}

func concurrentFlowWorker(stack *hub.Stack, tasks chan concurrentFlowIn, out chan concurrentFlowOut) {
	for taskDef := range tasks {
		result, _ := handleOneTask(copyFlowStack(stack), taskDef.taskDef)
		out <- concurrentFlowOut{taskDef: taskDef.taskDef, result: result}
	}
}

func waitConcurrentTaskResult(stack *hub.Stack, out chan concurrentFlowOut, counter int) (lastKey string) {
	results := make(map[string]interface{}, counter)
	for counter > 0 {
		//等待结果
		result := <-out
		key := result.taskDef.ResultKey
		if len(key) > 0 {
			results[key] = result.result
			lastKey = key
		}
		counter--
	}
	//防止并发读写crash
	for k, v := range results {
		stack.StepResult[k] = v
	}

	//由于并行，最后的结果并不确定，所以并行的返回结果不是固定的，因此当需要返回值时，最后一个应该是非并行的
	return lastKey
}

func Run(stack *hub.Stack) (interface{}, string, int) {
	var lastResultKey string
	var lastTypeKey string
	var counter int
	var in chan concurrentFlowIn
	var out chan concurrentFlowOut
	var result interface{}
	var code int

	flowDef, err := unit.FindFlowDef(stack, stack.ChildName)
	if flowDef == nil {
		klog.Errorln("获得Flow定义失败：", err)
		panic(err)
	}

	if flowDef.ConcurrentNum > 1 {
		in = make(chan concurrentFlowIn, len(flowDef.Tasks))
		defer close(in)
		out = make(chan concurrentFlowOut, len(flowDef.Tasks))
		defer close(out)
		for i := 0; i < flowDef.ConcurrentNum; i++ {
			go concurrentFlowWorker(stack, in, out)
		}
	}

	lastTypeKey = "json" //默认类型为json

	for i := range flowDef.Tasks {
		taskDef := flowDef.Tasks[i]
		if flowDef.ConcurrentNum > 1 {
			if taskDef.Concurrent {
				in <- concurrentFlowIn{taskDef: &taskDef}
				counter++
				continue
			} else {
				//避免并发读写ResultKey
				if counter > 0 {
					lastResultKey = waitConcurrentTaskResult(stack, out, counter)
					counter = 0
				}
			}
		}

		result, code = handleOneTask(stack, &taskDef)
		if code != 200 {
			return nil, "", code
		}

		if len(taskDef.ResultKey) > 0 {
			stack.StepResult[taskDef.ResultKey] = result
			lastResultKey = taskDef.ResultKey
			if taskDef.Response != nil {
				lastTypeKey = taskDef.Response.Type
			}
		}
	}

	//当最后一个step也是并行，等待全部执行完
	if counter > 0 {
		lastResultKey = waitConcurrentTaskResult(stack, out, counter)
	}

	//由于并行，最后的结果并不确定，所以并行的返回结果不是固定的，因此当需要返回值时，最后一个应该是非并行的
	return stack.StepResult[lastResultKey], lastTypeKey, http.StatusOK
}

func fillVars(stack *hub.Stack, private string, parameters *[]hub.OriginDefParam) {
	var value string
	if stack.StepResult[hub.VarsName] == nil {
		stack.StepResult[hub.VarsName] = make(map[string]interface{})
	}

	vars := stack.StepResult[hub.VarsName].(map[string]interface{})

	privateDef, err := unit.FindPrivateDef(stack, private, private)
	if err != nil {
		klog.Errorln("获得API定义失败：", err)
		panic(err)
	}

	for _, parameter := range *parameters {
		if parameter.In == hub.VarsName {
			if len(parameter.Value) > 0 {
				value = parameter.Value
			} else {
				value = unit.GetParameterValue(stack, privateDef, parameter.From)
			}

			oldValue, isOk := vars[parameter.Name]
			if isOk {
				klog.Infoln("fillVars replace ", parameter.Name, " from ", oldValue, " to ", value)
			}
			vars[parameter.Name] = value
			klog.Infoln("fillVars value: ", vars[parameter.Name])
		}
	}
}
