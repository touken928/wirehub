package architecture_test

import (
	"reflect"
	"testing"

	"github.com/touken928/wirehub/internal/api/http/dto"
	"github.com/touken928/wirehub/internal/service"
)

func TestMapHTTPContractsContainNoRepoTypes(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(service.MapInput{}),
		reflect.TypeOf(service.MapView{}),
		reflect.TypeOf(dto.MapResponse{}),
	} {
		if path := repoTypePath(typ, map[reflect.Type]bool{}); path != "" {
			t.Fatalf("%s recursively contains repository type %s", typ, path)
		}
	}
}

func TestExportedServiceMutationMethodsContainNoRepoTypes(t *testing.T) {
	appType := reflect.TypeOf((*service.App)(nil))
	for i := 0; i < appType.NumMethod(); i++ {
		method := appType.Method(i)
		if path := repoFuncSignaturePath(method.Type); path != "" {
			t.Fatalf("service method %s recursively contains repository type %s", method.Name, path)
		}
	}
}

func repoFuncSignaturePath(typ reflect.Type) string {
	seen := map[reflect.Type]bool{}
	for i := 1; i < typ.NumIn(); i++ {
		if path := repoTypePath(typ.In(i), seen); path != "" {
			return path
		}
	}
	for i := 0; i < typ.NumOut(); i++ {
		if path := repoTypePath(typ.Out(i), seen); path != "" {
			return path
		}
	}
	return ""
}

func repoTypePath(typ reflect.Type, seen map[reflect.Type]bool) string {
	if typ == nil {
		return ""
	}
	if typ.PkgPath() == modulePath+"/internal/repo" {
		return typ.String()
	}
	if seen[typ] {
		return ""
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Func:
		for i := 0; i < typ.NumIn(); i++ {
			if path := repoTypePath(typ.In(i), seen); path != "" {
				return path
			}
		}
		for i := 0; i < typ.NumOut(); i++ {
			if path := repoTypePath(typ.Out(i), seen); path != "" {
				return path
			}
		}
	case reflect.Interface:
		for i := 0; i < typ.NumMethod(); i++ {
			if path := repoTypePath(typ.Method(i).Type, seen); path != "" {
				return path
			}
		}
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return repoTypePath(typ.Elem(), seen)
	case reflect.Map:
		if path := repoTypePath(typ.Key(), seen); path != "" {
			return path
		}
		return repoTypePath(typ.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			if path := repoTypePath(typ.Field(i).Type, seen); path != "" {
				return path
			}
		}
	}
	return ""
}
