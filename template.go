package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"
	"runtime/cgo"
	"text/template"
	"unsafe"

	"github.com/drakedevel/go-text-template-napi/internal/napi"
)

type templateObj struct {
	inner *template.Template
}

func (tmpl *templateObj) wrap(env napi.Env, object napi.Value) error {
	// TODO: Type tagging
	handle := cgo.NewHandle(tmpl)
	// TODO: Don't leak finalizeData
	finalizeCb, finalizeData, _ := napi.MakeNapiFinalize(templateFinalize)
	// FIXME: Don't use Handle pointer
	return env.Wrap(object, unsafe.Pointer(&handle), finalizeCb, finalizeData)
}

func unwrapTemplate(env napi.Env, object napi.Value) (*templateObj, error) {
	// TODO: Type tagging
	wrapped, err := env.Unwrap(object)
	if err != nil {
		return nil, err
	}
	// TODO: Don't use Handle pointer
	handle := *(*cgo.Handle)(wrapped)
	return handle.Value().(*templateObj), nil
}

func extractString(env napi.Env, value napi.Value) (string, error) {
	// Get string length
	strLen, err := env.GetValueString(value, nil)
	if err != nil {
		return "", err
	}

	// Allocate buffer and get string contents
	buf := make([]byte, strLen+1)
	strLen, err = env.GetValueString(value, buf)
	if err != nil {
		return "", err
	}
	return string(buf[0:strLen]), nil
}

func extractBigint(env napi.Env, value napi.Value) (*big.Int, error) {
	// Get length
	wordCount := 0
	if err := env.GetValueBigintWords(value, nil, &wordCount, nil); err != nil {
		return nil, err
	}

	// Allocate space and get contents
	var signBit int
	words := make([]uint64, wordCount)
	if err := env.GetValueBigintWords(value, &signBit, &wordCount, &words[0]); err != nil {
		return nil, err
	}

	// Convert to big-endian bytes
	var buf bytes.Buffer
	for i := len(words) - 1; i >= 0; i-- {
		err := binary.Write(&buf, binary.BigEndian, words[i])
		if err != nil {
			return nil, err
		}
	}
	fmt.Println("Bigint words", words)
	fmt.Println("Bigint bytes", buf.Bytes())

	result := new(big.Int)
	result.SetBytes(buf.Bytes())
	if signBit > 0 {
		result.Neg(result)
	}
	return result, nil
}

func callbackEntry(env napi.Env, info napi.CallbackInfo, nArgs int) (napi.Value, []napi.Value, error) {
	var thisArg napi.Value
	argc := nArgs
	argv := make([]napi.Value, argc)
	var argvPtr *napi.Value
	if argc > 0 {
		argvPtr = &argv[0]
	}
	if err := env.GetCbInfo(info, &argc, argvPtr, &thisArg, nil); err != nil {
		return nil, nil, err
	}
	return thisArg, argv, nil
}

func templateMethodEntry(env napi.Env, info napi.CallbackInfo, nArgs int) (*templateObj, []napi.Value, error) {
	thisArg, argv, err := callbackEntry(env, info, nArgs)
	if err != nil {
		return nil, nil, err
	}

	// Retrieve wrapped native object from JS object
	this, err := unwrapTemplate(env, thisArg)
	if err != nil {
		return nil, nil, err
	}
	return this, argv, nil
}

type templateMethodFunc func(napi.Env, *templateObj, []napi.Value) (napi.Value, error)

func makeTemplateMethodCallback(fn templateMethodFunc, nArgs int) (napi.Callback, unsafe.Pointer, func()) {
	return napi.MakeNapiCallback(func(env napi.Env, info napi.CallbackInfo) (napi.Value, error) {
		this, args, err := templateMethodEntry(env, info, nArgs)
		if err != nil {
			return nil, err
		}
		return fn(env, this, args)
	})
}

func buildTemplateClass(env napi.Env) (napi.Value, error) {
	// Build property descriptors
	type method struct {
		fn    templateMethodFunc
		nArgs int
	}
	methods := map[string]method{
		// TODO: AddParseTree?
		// TODO: Clone
		// TODO: DefinedTemplates?
		// TODO: Delims
		"execute":         method{templateMethodExecute, 1},
		"executeTemplate": method{templateMethodExecuteTemplate, 2},
		// TODO: Funcs
		// TODO: Lookup
		"name": method{templateMethodName, 0},
		// TODO: New
		// TODO: Option
		"parse": method{templateMethodParse, 1},
		// TODO: ParseFS
		// TODO: ParseFiles
		// TODO: ParseGlob
		// TODO: Templates
	}
	var propDescs []napi.PropertyDescriptor
	for name, spec := range methods {
		// TODO: Don't leak cbData
		cb, cbData, _ := makeTemplateMethodCallback(spec.fn, spec.nArgs)
		nameObj, err := env.CreateString(name)
		if err != nil {
			return nil, err
		}
		propDescs = append(propDescs, napi.PropertyDescriptor{
			Name:       nameObj,
			Method:     cb,
			Attributes: napi.DefaultMethod,
			Data:       cbData,
		})
	}

	// Define class
	// TODO: Don't leak consData
	consCb, consData, _ := napi.MakeNapiCallback(templateConstructor)
	return env.DefineClass("Template", consCb, consData, propDescs)
}

func templateConstructor(env napi.Env, info napi.CallbackInfo) (napi.Value, error) {
	// TODO: Add check for new.target
	thisArg, argv, err := callbackEntry(env, info, 1)
	if err != nil {
		return nil, err
	}
	name, err := extractString(env, argv[0])
	if err != nil {
		return nil, err
	}

	// Create native object and attach to JS object
	data := templateObj{template.New(name)}
	if err := data.wrap(env, thisArg); err != nil {
		return nil, err
	}
	return nil, nil
}

func templateFinalize(env napi.Env, data unsafe.Pointer) error {
	fmt.Printf("In Template finalize\n")
	handle := *(*cgo.Handle)(data)
	handle.Delete()
	return nil
}

func convertTemplateData(env napi.Env, value napi.Value) (interface{}, error) {
	valueType, err := env.Typeof(value)
	if err != nil {
		return nil, err
	}
	switch valueType {
	case napi.Undefined, napi.Null:
		// TODO: Filter out Undefined from parent object, keep Null
		return nil, nil
	case napi.Boolean:
		return env.GetValueBool(value)
	case napi.Number:
		return env.GetValueDouble(value)
	case napi.String:
		return extractString(env, value)
	case napi.Object:
		isArray, err := env.IsArray(value)
		if err != nil {
			return nil, err
		}
		if isArray {
			length, err := env.GetArrayLength(value)
			if err != nil {
				return nil, err
			}
			result := make([]interface{}, length)
			var i uint32
			for i = 0; i < length; i++ {
				// TODO: Scope?
				elt, err := env.GetElement(value, i)
				if err != nil {
					return nil, err
				}
				eltConv, err := convertTemplateData(env, elt)
				if err != nil {
					return nil, err
				}
				result[i] = eltConv
			}
			return result, nil
		} else {
			// TODO: Should any other object types get special handling?
			propNames, err := env.GetAllPropertyNames(value, napi.KeyOwnOnly, napi.KeySkipSymbols, napi.KeyNumbersToStrings)
			if err != nil {
				return nil, err
			}
			length, err := env.GetArrayLength(propNames)
			if err != nil {
				return nil, err
			}
			result := map[string]interface{}{}
			var i uint32
			for i = 0; i < length; i++ {
				// TODO: Scope?
				key, err := env.GetElement(propNames, i)
				if err != nil {
					return nil, err
				}
				keyStr, err := extractString(env, key)
				if err != nil {
					return nil, err
				}
				elt, err := env.GetProperty(value, key)
				if err != nil {
					return nil, err
				}
				eltConv, err := convertTemplateData(env, elt)
				if err != nil {
					return nil, err
				}
				result[keyStr] = eltConv
			}
			return result, nil
		}
	case napi.Bigint:
		return extractBigint(env, value)
	default:
		// No useful way to map these to Go types
		// TODO: More useful error message?
		err = env.ThrowTypeError("ERR_INVALID_ARG_TYPE", "Unsupported value type")
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("threw exception")
	}
}

func templateMethodExecute(env napi.Env, this *templateObj, args []napi.Value) (napi.Value, error) {
	// TODO: Allow passing in a stream?
	data, err := convertTemplateData(env, args[0])
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := this.inner.Execute(&buf, data); err != nil {
		// TODO: Map to better JS error?
		return nil, err
	}
	return env.CreateString(buf.String())
}

func templateMethodExecuteTemplate(env napi.Env, this *templateObj, args []napi.Value) (napi.Value, error) {
	// TODO: Allow passing in a stream?
	name, err := extractString(env, args[0])
	if err != nil {
		return nil, err
	}
	data, err := convertTemplateData(env, args[1])
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := this.inner.ExecuteTemplate(&buf, name, data); err != nil {
		// TODO: Map to better JS error?
		return nil, err
	}
	return env.CreateString(buf.String())
}

func templateMethodName(env napi.Env, this *templateObj, args []napi.Value) (napi.Value, error) {
	return env.CreateString(this.inner.Name())
}

func templateMethodParse(env napi.Env, this *templateObj, args []napi.Value) (napi.Value, error) {
	text, err := extractString(env, args[0])
	if err != nil {
		return nil, err
	}
	result, err := this.inner.Parse(text)
	if err != nil {
		// TODO: Map to better JS error?
		return nil, err
	}
	if result != this.inner {
		panic("Expected Parse to return itself")
	}
	return nil, nil // XXX: Should return this
}
