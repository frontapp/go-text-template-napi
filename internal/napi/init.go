package napi

// #cgo linux LDFLAGS: -Wl,--unresolved-symbols=ignore-all
// #cgo darwin LDFLAGS: -Wl,-undefined,dynamic_lookup
// #include <node_api.h>
// napi_value napiModuleInit(napi_env env, napi_value exports);
// NAPI_MODULE(go_text_template_napi_binding, napiModuleInit)
import "C"

var registeredInitFunc func(env Env, exports Value) (Value, error)

//export napiModuleInit
func napiModuleInit(rawEnv C.napi_env, exports C.napi_value) C.napi_value {
	env := Env{rawEnv}
	if registeredInitFunc == nil {
		panic("Module init function not registered")
	}
	result, err := registeredInitFunc(env, Value(exports))
	if err != nil {
		env.maybeThrowError(err)
		return nil
	}
	return C.napi_value(result)
}

func SetModuleInit(init func(env Env, exports Value) (Value, error)) {
	if registeredInitFunc != nil {
		panic("Module init function already registered")
	}
	registeredInitFunc = init
}

func SetInstanceData(env Env, data interface{}, finalizeFunc finalizeFunc) error {
	ptr, finalize, finalizeHint := makeDataAndFinalize(data, finalizeFunc)
	return env.SetInstanceData(ptr, finalize, finalizeHint)
}

func GetInstanceData(env Env) (interface{}, error) {
	ptr, err := env.GetInstanceData()
	if err != nil {
		return nil, err
	}
	return unlaunderHandle(ptr).Value(), nil
}
