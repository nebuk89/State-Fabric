package canonical

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

// Marshal implements the fabric-json-v0 canonical encoding used for object
// identities and signatures. Canonical values intentionally exclude maps,
// floating-point numbers, and interface-typed fields.
func Marshal(value any) ([]byte, error) {
	if err := validateShape(reflect.ValueOf(value)); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode canonical JSON: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}

func Decode(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode canonical JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("decode canonical JSON: trailing data")
	}
	if err := validateShape(reflect.ValueOf(destination)); err != nil {
		return err
	}
	return nil
}

func validateShape(value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return validateShape(value.Elem())
	}

	switch value.Kind() {
	case reflect.Map:
		return errors.New("canonical JSON does not allow maps")
	case reflect.Float32, reflect.Float64:
		return errors.New("canonical JSON does not allow floating-point numbers")
	case reflect.Interface:
		if value.IsNil() {
			return nil
		}
		return errors.New("canonical JSON does not allow interface-typed values")
	case reflect.Struct:
		for index := range value.NumField() {
			field := value.Type().Field(index)
			if field.PkgPath != "" {
				continue
			}
			if err := validateShape(value.Field(index)); err != nil {
				return fmt.Errorf("%s: %w", field.Name, err)
			}
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		for index := range value.Len() {
			if err := validateShape(value.Index(index)); err != nil {
				return fmt.Errorf("index %d: %w", index, err)
			}
		}
	}
	return nil
}
