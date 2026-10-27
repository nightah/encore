package api

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// Lists and maps in responses are never null: an empty one is [] or {}. The schema says so (arrays are not nullable), and fillEmpty makes it true for values built from nil Go slices and maps.
func init() {
	huma.DefaultArrayNullable = false
}

func nonNilTransformer(_ huma.Context, _ string, v any) (any, error) {
	return fillEmpty(v), nil
}

// fillEmpty works at any depth on a copy, so shared or cached values stay untouched. Byte slices are left alone.
func fillEmpty(v any) any {
	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)
	if !needsFill(rv.Type(), map[reflect.Type]bool{}) {
		return v
	}

	return fill(rv).Interface()
}

func fill(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || !needsFill(v.Type().Elem(), map[reflect.Type]bool{}) {
			return v
		}

		out := reflect.New(v.Type().Elem())
		out.Elem().Set(fill(v.Elem()))

		return out
	case reflect.Struct:
		if !needsFill(v.Type(), map[reflect.Type]bool{}) {
			return v
		}

		out := reflect.New(v.Type()).Elem()
		out.Set(v)

		for i := range v.NumField() {
			if f := out.Field(i); f.CanSet() {
				f.Set(fill(v.Field(i)))
			}
		}

		return out
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v
		}

		if v.IsNil() {
			return reflect.MakeSlice(v.Type(), 0, 0)
		}

		if !needsFill(v.Type().Elem(), map[reflect.Type]bool{}) {
			return v
		}

		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(fill(v.Index(i)))
		}

		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.MakeMap(v.Type())
		}

		if !needsFill(v.Type().Elem(), map[reflect.Type]bool{}) {
			return v
		}

		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for it := v.MapRange(); it.Next(); {
			out.SetMapIndex(it.Key(), fill(it.Value()))
		}

		return out
	default:
		return v
	}
}

// needsFill reports whether a value of type t can contain a nil slice or map that fill would replace.
func needsFill(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}

	seen[t] = true

	switch t.Kind() {
	case reflect.Slice:
		return t.Elem().Kind() != reflect.Uint8
	case reflect.Map:
		return true
	case reflect.Pointer:
		return needsFill(t.Elem(), seen)
	case reflect.Struct:
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() && needsFill(f.Type, seen) {
				return true
			}
		}

		return false
	default:
		return false
	}
}
