package gorm

import (
	"database/sql"
	"reflect"
	"strings"

	"gorm.io/gorm/schema"
)

func Scan(rows *sql.Rows, db *DB, initialized bool) {
	columns, _ := rows.Columns()
	values := make([]interface{}, len(columns))

	switch dest := db.Statement.Dest.(type) {
	case map[string]interface{}, *map[string]interface{}:
		if initialized || rows.Next() {
			for idx := range columns {
				values[idx] = new(interface{})
			}

			db.RowsAffected++
			db.AddError(rows.Scan(values...))

			mapValue, ok := dest.(map[string]interface{})
			if !ok {
				if v, ok := dest.(*map[string]interface{}); ok {
					mapValue = *v
				}
			}

			for idx, column := range columns {
				if v, ok := values[idx].(*interface{}); ok {
					if v == nil {
						mapValue[column] = nil
					} else {
						mapValue[column] = *v
					}
				}
			}
		}
	case *[]map[string]interface{}:
		for initialized || rows.Next() {
			for idx := range columns {
				values[idx] = new(interface{})
			}

			initialized = false
			db.RowsAffected++
			db.AddError(rows.Scan(values...))

			mapValue := map[string]interface{}{}
			for idx, column := range columns {
				if v, ok := values[idx].(*interface{}); ok {
					if v == nil {
						mapValue[column] = nil
					} else {
						mapValue[column] = *v
					}
				}
			}

			*dest = append(*dest, mapValue)
		}
	case *int, *int64, *uint, *uint64:
		for initialized || rows.Next() {
			initialized = false
			db.RowsAffected++
			db.AddError(rows.Scan(dest))
		}
	default:
		switch db.Statement.ReflectValue.Kind() {
		case reflect.Slice, reflect.Array:
			reflectValueType := db.Statement.ReflectValue.Type().Elem()
			isPtr := reflectValueType.Kind() == reflect.Ptr
			if isPtr {
				reflectValueType = reflectValueType.Elem()
			}

			db.Statement.ReflectValue.Set(reflect.MakeSlice(db.Statement.ReflectValue.Type(), 0, 0))
			fields := make([]*schema.Field, len(columns))
			joinFields := make([][2]*schema.Field, len(columns))

			for idx, column := range columns {
				if field := db.Statement.Schema.LookUpField(column); field != nil && field.Readable {
					fields[idx] = field
				} else if names := strings.Split(column, "__"); len(names) > 1 {
					if rel, ok := db.Statement.Schema.Relationships.Relations[names[0]]; ok {
						if field := rel.FieldSchema.LookUpField(strings.Join(names[1:], "__")); field != nil && field.Readable {
							fields[idx] = field
							joinFields[idx] = [2]*schema.Field{rel.Field, field}
							continue
						}
					}
					values[idx] = &sql.RawBytes{}
				} else {
					values[idx] = &sql.RawBytes{}
				}
			}

			for initialized || rows.Next() {
				initialized = false
				db.RowsAffected++

				elem := reflect.New(reflectValueType).Elem()
				if reflectValueType.Kind() != reflect.Struct && len(fields) == 1 {
					// pluck
					values[0] = elem.Addr().Interface()
					db.AddError(rows.Scan(values...))
				} else {
					for idx, field := range fields {
						if field != nil {
							values[idx] = reflect.New(reflect.PtrTo(field.FieldType)).Interface()
						}
					}

					db.AddError(rows.Scan(values...))

					for idx, field := range fields {
						if joinFields[idx][0] != nil {
							value := reflect.ValueOf(values[idx]).Elem()
							relValue := joinFields[idx][0].ReflectValueOf(elem)

							if relValue.Kind() == reflect.Ptr && relValue.IsNil() {
								if value.IsNil() {
									continue
								}
								relValue.Set(reflect.New(relValue.Type().Elem()))
							}

							field.Set(relValue, values[idx])
						} else if field != nil {
							field.Set(elem, values[idx])
						}
					}
				}

				if isPtr {
					db.Statement.ReflectValue.Set(reflect.Append(db.Statement.ReflectValue, elem.Addr()))
				} else {
					db.Statement.ReflectValue.Set(reflect.Append(db.Statement.ReflectValue, elem))
				}
			}
		case reflect.Struct:
			if initialized || rows.Next() {
				for idx, column := range columns {
					if field := db.Statement.Schema.LookUpField(column); field != nil && field.Readable {
						values[idx] = reflect.New(reflect.PtrTo(field.FieldType)).Interface()
					} else if names := strings.Split(column, "__"); len(names) > 1 {
						if rel, ok := db.Statement.Schema.Relationships.Relations[names[0]]; ok {
							if field := rel.FieldSchema.LookUpField(strings.Join(names[1:], "__")); field != nil && field.Readable {
								values[idx] = reflect.New(reflect.PtrTo(field.FieldType)).Interface()
								continue
							}
						}
						values[idx] = &sql.RawBytes{}
					} else {
						values[idx] = &sql.RawBytes{}
					}
				}

				db.RowsAffected++
				db.AddError(rows.Scan(values...))

				for idx, column := range columns {
					if field := db.Statement.Schema.LookUpField(column); field != nil && field.Readable {
						field.Set(db.Statement.ReflectValue, values[idx])
					} else if names := strings.Split(column, "__"); len(names) > 1 {
						if rel, ok := db.Statement.Schema.Relationships.Relations[names[0]]; ok {
							relValue := rel.Field.ReflectValueOf(db.Statement.ReflectValue)
							if field := rel.FieldSchema.LookUpField(strings.Join(names[1:], "__")); field != nil && field.Readable {
								value := reflect.ValueOf(values[idx]).Elem()

								if relValue.Kind() == reflect.Ptr && relValue.IsNil() {
									if value.IsNil() {
										continue
									}
									relValue.Set(reflect.New(relValue.Type().Elem()))
								}

								field.Set(relValue, values[idx])
							}
						}
					}
				}
			}
		}
	}

	if db.RowsAffected == 0 && db.Statement.RaiseErrorOnNotFound {
		db.AddError(ErrRecordNotFound)
	}
}
