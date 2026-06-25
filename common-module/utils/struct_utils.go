package utils

import (
	"fmt"
	"reflect"
	"strings"
)

// MapStructFields maps fields from source struct to destination struct
// based on matching field names and types
func MapStructFields(source, dest interface{}) error {
	sourceVal := reflect.ValueOf(source)
	destVal := reflect.ValueOf(dest)

	// Check if both are pointers to structs
	if sourceVal.Kind() != reflect.Ptr || sourceVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("source must be a pointer to struct")
	}
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("destination must be a pointer to struct")
	}

	sourceElem := sourceVal.Elem()
	destElem := destVal.Elem()
	sourceType := sourceElem.Type()
	destType := destElem.Type()

	// Create a map of destination field names and types for quick lookup
	destFields := make(map[string]reflect.Type)
	for i := 0; i < destType.NumField(); i++ {
		field := destType.Field(i)
		destFields[field.Name] = field.Type
	}

	// Iterate through source fields and map matching ones
	for i := 0; i < sourceType.NumField(); i++ {
		sourceField := sourceType.Field(i)
		sourceFieldVal := sourceElem.Field(i)

		// Check if destination has a field with the same name and type
		if destFieldType, exists := destFields[sourceField.Name]; exists {
			if sourceField.Type == destFieldType {
				destField := destElem.FieldByName(sourceField.Name)
				if destField.IsValid() && destField.CanSet() {
					destField.Set(sourceFieldVal)
				}
			}
		}
	}

	return nil
}

// MapStructFieldsWithTag maps fields from source struct to destination struct
// based on matching field names, types, and optional tag matching
func MapStructFieldsWithTag(source, dest interface{}, tagName string) error {
	sourceVal := reflect.ValueOf(source)
	destVal := reflect.ValueOf(dest)

	// Check if both are pointers to structs
	if sourceVal.Kind() != reflect.Ptr || sourceVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("source must be a pointer to struct")
	}
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("destination must be a pointer to struct")
	}

	sourceElem := sourceVal.Elem()
	destElem := destVal.Elem()
	sourceType := sourceElem.Type()
	destType := destElem.Type()

	// Create maps for destination fields (by name and by tag)
	destFieldsByName := make(map[string]reflect.StructField)
	destFieldsByTag := make(map[string]reflect.StructField)

	for i := 0; i < destType.NumField(); i++ {
		field := destType.Field(i)
		destFieldsByName[field.Name] = field

		if tagName != "" {
			if tag := field.Tag.Get(tagName); tag != "" {
				// Handle comma-separated tags (e.g., "json:name,omitempty")
				tagValue := strings.Split(tag, ",")[0]
				destFieldsByTag[tagValue] = field
			}
		}
	}

	// Iterate through source fields and map matching ones
	for i := 0; i < sourceType.NumField(); i++ {
		sourceField := sourceType.Field(i)
		sourceFieldVal := sourceElem.Field(i)

		// Try to find matching field by name first
		if destField, exists := destFieldsByName[sourceField.Name]; exists {
			if sourceField.Type == destField.Type {
				destFieldVal := destElem.FieldByName(sourceField.Name)
				if destFieldVal.IsValid() && destFieldVal.CanSet() {
					destFieldVal.Set(sourceFieldVal)
				}
			}
		} else if tagName != "" {
			// Try to find matching field by tag
			if sourceTag := sourceField.Tag.Get(tagName); sourceTag != "" {
				tagValue := strings.Split(sourceTag, ",")[0]
				if destField, exists := destFieldsByTag[tagValue]; exists {
					if sourceField.Type == destField.Type {
						destFieldVal := destElem.FieldByName(destField.Name)
						if destFieldVal.IsValid() && destFieldVal.CanSet() {
							destFieldVal.Set(sourceFieldVal)
						}
					}
				}
			}
		}
	}

	return nil
}

// MapStructFieldsWithCustomMapping maps fields using a custom mapping function
func MapStructFieldsWithCustomMapping(source, dest interface{}, mappingFunc func(sourceField, destField reflect.StructField) bool) error {
	sourceVal := reflect.ValueOf(source)
	destVal := reflect.ValueOf(dest)

	// Check if both are pointers to structs
	if sourceVal.Kind() != reflect.Ptr || sourceVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("source must be a pointer to struct")
	}
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("destination must be a pointer to struct")
	}

	sourceElem := sourceVal.Elem()
	destElem := destVal.Elem()
	sourceType := sourceElem.Type()
	destType := destElem.Type()

	// Create a slice of destination fields
	var destFields []reflect.StructField
	for i := 0; i < destType.NumField(); i++ {
		destFields = append(destFields, destType.Field(i))
	}

	// Iterate through source fields and map using custom mapping function
	for i := 0; i < sourceType.NumField(); i++ {
		sourceField := sourceType.Field(i)
		sourceFieldVal := sourceElem.Field(i)

		// Find matching destination field using custom mapping function
		for _, destField := range destFields {
			if mappingFunc(sourceField, destField) {
				destFieldVal := destElem.FieldByName(destField.Name)
				if destFieldVal.IsValid() && destFieldVal.CanSet() {
					destFieldVal.Set(sourceFieldVal)
				}
				break
			}
		}
	}

	return nil
}
