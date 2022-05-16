package unit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"plugin"
	"strings"

	klog "k8s.io/klog/v2"

	"github.com/jasony62/tms-go-apihub/hub"
	"github.com/jasony62/tms-go-apihub/util"
)

const (
	JSON_TYPE_API = iota
	JSON_TYPE_FLOW
	JSON_TYPE_SCHEDULE
	JSON_TYPE_PRIVATE
)

func FindPrivateData(bucket string, name string) (*hub.PrivateArray, error) {
	key := initBucketKey(bucket, name)
	value, ok := hub.DefaultApp.PrivateMap[key]
	if !ok {
		return nil, errors.New("Not found private data")
	}
	return value, nil
}

func FindApiDef(stack *hub.Stack, id string) (*hub.ApiDef, error) {
	var err error
	key, bucket := GetBucketKey(stack, id)
	value, ok := hub.DefaultApp.ApiMap[key]
	if !ok {
		return nil, errors.New("Not found api")
	}

	apiDef := value
	if len(apiDef.PrivateName) > 0 {
		//需要load秘钥
		apiDef.Privates, err = FindPrivateData(bucket, apiDef.PrivateName)
		if err != nil {
			str := "获得Private数据失败：" + err.Error()
			klog.Errorln(str)
			panic(str)
		}
	}

	return apiDef, nil
}

func FindFlowDef(stack *hub.Stack, id string) (*hub.FlowDef, error) {
	key, _ := GetBucketKey(stack, id)
	value, ok := hub.DefaultApp.FlowMap[key]
	if !ok {
		return nil, errors.New("Not found flow")
	}
	return value, nil
}

func FindScheduleDef(stack *hub.Stack, id string) (*hub.ScheduleDef, error) {
	key, _ := GetBucketKey(stack, id)
	value, ok := hub.DefaultApp.ScheduleMap[key]
	if !ok {
		return nil, errors.New("Not found schedule")
	}
	return value, nil
}

func findPrivateValue(private *hub.PrivateArray, name string) string {
	for _, pair := range *private.Pairs {
		if pair.Name == name {
			return pair.Value
		}
	}
	return ""
}

func getArgsVal(stepResult map[string]interface{}, args []string) []string {
	vars := (stepResult["vars"]).(map[string]string)
	argsV := []string{}
	for _, v := range args {
		argsV = append(argsV, vars[v])
	}
	return argsV
}

func GetParameterValue(stack *hub.Stack, private *hub.PrivateArray, from *hub.ApiDefParamFrom) string {
	var value string
	switch from.From {
	case "query":
		value = stack.Query(from.Name)
	case hub.OriginName:
		value = stack.QueryFromStepResult("{{.origin." + from.Name + "}}")
	case "private":
		value = findPrivateValue(private, from.Name)
	case "template":
		value = stack.QueryFromStepResult(from.Name)
	case "StepResult":
		value = stack.QueryFromStepResult("{{." + from.Name + "}}")
	case "JsonTemplate":
		jsonOutBody := util.Json2Json(stack.StepResult, from.Template)
		byteJson, _ := json.Marshal(jsonOutBody)
		value = util.RemoveOutideQuote(byteJson)
	case "func":
		function := hub.FuncMap[from.Name]
		if function == nil {
			str := "获取function定义失败："
			klog.Errorln(str)
			panic(str)
		}
		switch funcV := function.(type) {
		case func() string:
			value = funcV()
		case func([]string) string:
			strs := strings.Fields(from.Args)
			argsV := getArgsVal(stack.StepResult, strs)
			value = funcV(argsV)
		default:
			str := from.Name + "不能执行"
			klog.Errorln(str)
			panic(str)
		}
	default:
		str := "不支持的type" + from.From
		klog.Errorln(str)
		panic(str)
	}
	return value
}

func LoadConfigJsonData(paths []string) {
	hub.DefaultApp.ApiMap = make(map[string]*hub.ApiDef)
	hub.DefaultApp.FlowMap = make(map[string]*hub.FlowDef)
	hub.DefaultApp.ScheduleMap = make(map[string]*hub.ScheduleDef)
	hub.DefaultApp.PrivateMap = make(map[string]*hub.PrivateArray)

	klog.Infoln("加载API def文件...")
	LoadJsonDefData(JSON_TYPE_API, paths[JSON_TYPE_API], "")
	klog.Infoln("\r\n")
	klog.Infoln("加载Flow def文件...")
	LoadJsonDefData(JSON_TYPE_FLOW, paths[JSON_TYPE_FLOW], "")
	klog.Infoln("\r\n")
	klog.Infoln("加载Schedule def文件...")
	LoadJsonDefData(JSON_TYPE_SCHEDULE, paths[JSON_TYPE_SCHEDULE], "")
	klog.Infoln("\r\n")
	klog.Infoln("加载Private def文件...")
	LoadJsonDefData(JSON_TYPE_PRIVATE, paths[JSON_TYPE_PRIVATE], "")
}

func LoadJsonDefData(jsonType int, path string, prefix string) {
	fileInfoList, err := ioutil.ReadDir(path)
	if err != nil {
		klog.Errorln(err)
		return
	}

	num := len(fileInfoList)

	klog.Infoln("\r\n")
	klog.Infoln("加载Json def文件，本目录文件数: ", num)

	oldPrefix := prefix
	for i := range fileInfoList {
		fileName := fmt.Sprintf("%s/%s", path, fileInfoList[i].Name())
		klog.Infoln("Json file: ", fileName)

		if fileInfoList[i].IsDir() {
			klog.Infoln("Json子目录: ", fileName)
			prefix = fileInfoList[i].Name()
			LoadJsonDefData(jsonType, path+"/"+prefix, prefix)
			klog.Infoln("\r\n")
		} else {
			prefix = oldPrefix

			byteFile, err := ioutil.ReadFile(fileName)
			if err != nil {
				str := "获得Json定义失败：" + err.Error()
				klog.Errorln(str)
				panic(str)
			}

			if !json.Valid(byteFile) {
				str := "Json文件无效：" + fileName
				klog.Errorln(str)
				panic(str)
			}

			var key string
			fname := fileInfoList[i].Name()
			index := strings.Index(fname, ".json")
			if index >= 0 {
				fname = fname[:index]
			}

			if prefix == "" {
				key = fname
			} else {
				key = prefix + "/" + fname
			}

			decoder := json.NewDecoder(bytes.NewReader(byteFile))
			switch jsonType {
			case JSON_TYPE_API:
				def := new(hub.ApiDef)
				decoder.Decode(&def)
				hub.DefaultApp.ApiMap[key] = def
			case JSON_TYPE_FLOW:
				def := new(hub.FlowDef)
				decoder.Decode(&def)
				hub.DefaultApp.FlowMap[key] = def
			case JSON_TYPE_SCHEDULE:
				def := new(hub.ScheduleDef)
				decoder.Decode(&def)
				hub.DefaultApp.ScheduleMap[key] = def
			case JSON_TYPE_PRIVATE:
				def := new(hub.PrivateArray)
				decoder.Decode(&def)
				hub.DefaultApp.PrivateMap[key] = def
			default:
			}

			klog.Infof("加载Json文件成功: key: %s\r\n", key)
		}
	}
}

func LoadConfigPluginData(path string) {
	fileInfoList, err := ioutil.ReadDir(path)
	if err != nil {
		klog.Errorln(err)
		return
	}

	num := len(fileInfoList)

	klog.Infoln("\r\n")
	klog.Infoln("Plugin def文件，本目录文件数: ", num)
	var prefix string
	for i := range fileInfoList {
		fileName := fmt.Sprintf("%s/%s", path, fileInfoList[i].Name())
		klog.Infoln("Json file: ", fileName)

		if fileInfoList[i].IsDir() {
			klog.Infoln("Json子目录: ", fileName)
			prefix = fileInfoList[i].Name()
			LoadConfigPluginData(path + "/" + prefix)
			klog.Infoln("\r\n")
		} else {
			if !strings.HasSuffix(fileName, ".so") {
				continue
			}
			p, err := plugin.Open(fileName)
			if err != nil {
				klog.Errorln(err)
				panic(err)
			}

			// 导入动态库，注册函数到hub.FuncMap
			registerFunc, err := p.Lookup("register")
			if err != nil {
				klog.Errorln(err)
				panic(err)
			}
			registerFunc.(func())()

			klog.Infof("加载Json文件成功！\r\n")
		}
	}
}

func initBucketKey(bucket string, fileName string) string {
	var key string
	if bucket == "" {
		key = fileName
	} else {
		key = bucket + "/" + fileName
	}
	return key
}

func GetBucketKey(stack *hub.Stack, fileName string) (string, string) {
	var bucket string
	if hub.DefaultApp.BucketEnable {
		bucket = stack.GinContext.Param(`bucket`)
	}

	key := initBucketKey(bucket, fileName)
	klog.Infof("GetBucketKey key: %s, bucket: %s", key, bucket)
	return key, bucket
}
