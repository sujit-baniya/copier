package copier

import (
	"database/sql"
	"errors"
	"reflect"
)

// Copy copy things
func Copy(toValue interface{}, fromValue interface{}) (err error) {
	var (
		isSlice bool
		amount  = 1
		from    = indirect(reflect.ValueOf(fromValue))
		to      = indirect(reflect.ValueOf(toValue))
	)

	if !to.CanAddr() {
		return errors.New("copy to value is unaddressable")
	}

	// Return is from value is invalid
	if !from.IsValid() {
		return
	}

	fromType := indirectType(from.Type())
	toType := indirectType(to.Type())

	// Just set it if possible to assign
	// And need to do copy anyway if the type is struct
	if fromType.Kind() != reflect.Struct && from.Type().AssignableTo(to.Type()) {
		to.Set(from)
		return
	}

	if to.Kind() == reflect.Slice {
		isSlice = true
		if from.Kind() == reflect.Slice {
			amount = from.Len()
		}
	}

	if (fromType.Kind() != reflect.Struct || toType.Kind() != reflect.Struct) && !(isSlice && fromType.ConvertibleTo(toType)) {
		return
	}

	// Going from empty slice to empty slice
	if amount == 0 {
		to.Set(reflect.MakeSlice(to.Type(), 0, 0))
	}

	for i := 0; i < amount; i++ {
		var dest, source reflect.Value

		if isSlice {
			// source
			if from.Kind() == reflect.Slice {
				source = indirect(from.Index(i))
			} else {
				source = indirect(from)
			}
			// dest
			dest = indirect(reflect.New(toType).Elem())
		} else {
			source = indirect(from)
			dest = indirect(to)
		}

		// check source
		if source.IsValid() {
			if fromType.Kind() != reflect.Struct && toType.Kind() != reflect.Struct {
				set(dest, source)
			} else {
				needInitFields := map[string]struct{}{}
				fromTypeFields := deepFields(fromType, source, ``, needInitFields)
				//fmt.Printf("%#v", fromTypeFields)
				InitNilFields(toType, dest, ``, needInitFields)
				// Copy from field to field or method
				for _, field := range fromTypeFields {
					name := field.Name

					if fromField := source.FieldByName(name); fromField.IsValid() {
						// has field
						if toField := dest.FieldByName(name); toField.IsValid() {
							if toField.CanSet() {
								if !set(toField, fromField) {
									if err := Copy(toField.Addr().Interface(), fromField.Interface()); err != nil {
										return err
									}
								}
							}
						} else {
							// try to set to method
							var toMethod reflect.Value
							if dest.CanAddr() {
								toMethod = dest.Addr().MethodByName(name)
							} else {
								toMethod = dest.MethodByName(name)
							}

							if toMethod.IsValid() && toMethod.Type().NumIn() == 1 && fromField.Type().AssignableTo(toMethod.Type().In(0)) {
								toMethod.Call([]reflect.Value{fromField})
							}
						}
					}
				}

				// Copy from method to field
				for _, field := range deepFields(toType, dest, ``, nil) {
					name := field.Name

					var fromMethod reflect.Value
					if source.CanAddr() {
						fromMethod = source.Addr().MethodByName(name)
					} else {
						fromMethod = source.MethodByName(name)
					}

					if fromMethod.IsValid() && fromMethod.Type().NumIn() == 0 && fromMethod.Type().NumOut() == 1 {
						if toField := dest.FieldByName(name); toField.IsValid() && toField.CanSet() {
							values := fromMethod.Call([]reflect.Value{})
							if len(values) >= 1 {
								set(toField, values[0])
							}
						}
					}
				}
			}
		}
		if isSlice {
			if dest.Addr().Type().AssignableTo(to.Type().Elem()) {
				to.Set(reflect.Append(to, dest.Addr()))
			} else if dest.Type().AssignableTo(to.Type().Elem()) {
				to.Set(reflect.Append(to, dest))
			}
		}
	}
	return
}

func deepFields(reflectType reflect.Type, reflectValue reflect.Value, prefix string, needInitFields map[string]struct{}) []reflect.StructField {
	var fields []reflect.StructField

	if reflectType = indirectType(reflectType); reflectType.Kind() == reflect.Struct {
		for i := 0; i < reflectType.NumField(); i++ {
			v := reflectType.Field(i)
			if v.Anonymous {
				value := indirect(reflectValue).Field(i)
				if value.Kind() == reflect.Ptr {
					if value.IsNil() {
						continue
					}
					if needInitFields != nil {
						needInitFields[prefix+v.Name] = struct{}{}
					}
				}
				prefix += v.Name + `.`
				fields = append(fields, deepFields(v.Type, value, prefix, needInitFields)...)
			} else {
				fields = append(fields, v)
			}
		}
	}

	return fields
}

// AllNilFields 初始化所有nil字段
var AllNilFields = map[string]struct{}{}

// InitNilFields initializes nil fields
func InitNilFields(reflectType reflect.Type, reflectValue reflect.Value, prefix string, needInitFields map[string]struct{}) {
	if needInitFields == nil {
		return
	}
	reflectType = indirectType(reflectType)
	if reflectType.Kind() != reflect.Struct {
		return
	}
	isAll := AllNilFields == needInitFields
	for i := 0; i < reflectType.NumField(); i++ {
		v := reflectType.Field(i)
		if !isAll {
			if _, ok := needInitFields[prefix+v.Name]; !ok {
				continue
			}
		}
		if !v.Anonymous {
			continue
		}
		value := indirect(reflectValue).Field(i)
		if value.Kind() != reflect.Ptr {
			continue
		}
		if !value.IsNil() {
			continue
		}
		if !value.CanSet() {
			continue
		}
		value.Set(reflect.New(v.Type.Elem()))
		prefix += v.Name + `.`
		InitNilFields(v.Type, value, prefix, needInitFields)
	}
}

func indirect(reflectValue reflect.Value) reflect.Value {
	for reflectValue.Kind() == reflect.Ptr {
		reflectValue = reflectValue.Elem()
	}
	return reflectValue
}

func indirectType(reflectType reflect.Type) reflect.Type {
	for reflectType.Kind() == reflect.Ptr || reflectType.Kind() == reflect.Slice {
		reflectType = reflectType.Elem()
	}
	return reflectType
}

func set(to, from reflect.Value) bool {
	if from.IsValid() {
		if to.Kind() == reflect.Ptr {
			//set `to` to nil if from is nil
			if from.Kind() == reflect.Ptr && from.IsNil() {
				to.Set(reflect.Zero(to.Type()))
				return true
			} else if to.IsNil() {
				to.Set(reflect.New(to.Type().Elem()))
			}
			to = to.Elem()
		}

		if from.Type().ConvertibleTo(to.Type()) {
			to.Set(from.Convert(to.Type()))
		} else if scanner, ok := to.Addr().Interface().(sql.Scanner); ok {
			err := scanner.Scan(from.Interface())
			if err != nil {
				return false
			}
		} else if from.Kind() == reflect.Ptr {
			return set(to, from.Elem())
		} else {
			return false
		}
	}
	return true
}
