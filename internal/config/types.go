package config

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"google.golang.org/protobuf/types/known/structpb"
)

// Script is the top-level struct that yaml script files will be deserialized into.
type Script struct {
	Name   string
	Weight uint
	Steps  []ScriptStep
}

// ScriptStep is a single step of a thumper script, for example a single call to CheckPermissions.
// TODO: it would be good to break this down into separate types/interfaces
// so that it's not just one ur-type - i.e. discriminated union or something
type ScriptStep struct {
	Op                   string
	Resource             string
	Subject              string
	Permission           string
	ExpectNoPermission   bool   `yaml:"expectNoPermission"`
	ExpectPermissionship string `yaml:"expectPermissionship"`
	NumExpected          uint   `yaml:"numExpected"`
	Updates              []Update
	Checks               []Check
	Schema               string
	Consistency          string
	Context              *ProtoStruct
}

// Check is one of a set of Checks handed to CheckBulk
type Check struct {
	Resource             string
	Subject              string
	Permission           string
	Context              *ProtoStruct
	ExpectNoPermission   bool   `yaml:"expectNoPermission"`
	ExpectPermissionship string `yaml:"expectPermissionship"`
}

// Update is a mutation to a single relationship in a WriteRelationships call.
// Op can be TOUCH, CREATE, or DELETE.
type Update struct {
	Op       string
	Resource string
	Subject  string
	Relation string
	Caveat   *CaveatContext
}

// ScriptVariables are the variables which can be replaced in a yaml file using
// go template notation, e.g. {{ .Prefix }} or {{ .RandomObjectID }}.
type ScriptVariables struct {
	Prefix      string
	IsMigration bool
}

// ProtoStruct is a wrapper around structpb.Struct which implements yaml.Unmarshaler.
type ProtoStruct structpb.Struct

// CaveatContext is a wrapper around a caveat name and ProtoStruct to support yaml unmarshaling.
type CaveatContext struct {
	Name    string
	Context *ProtoStruct
}

func (p *ProtoStruct) UnmarshalYAML(b []byte) error {
	c := make(map[string]interface{})

	if err := yaml.Unmarshal(b, &c); err != nil {
		return fmt.Errorf("failed to decode struct: %w", err)
	}

	converted, err := convertObject("", c)
	if err != nil {
		return fmt.Errorf("failed to convert struct: %w", err)
	}
	p.Fields = converted.Fields

	return nil
}

func convertObject(path string, obj map[string]interface{}) (*structpb.Struct, error) {
	fields := make(map[string]*structpb.Value, len(obj))

	for k, v := range obj {
		value, err := convertValue(path+"."+k, v)
		if err != nil {
			return nil, err
		}

		fields[k] = value
	}

	return &structpb.Struct{
		Fields: fields,
	}, nil
}

func convertList(path string, l []interface{}) (*structpb.ListValue, error) {
	values := make([]*structpb.Value, len(l))

	for i, v := range l {
		value, err := convertValue(path+"["+fmt.Sprint(i)+"]", v)
		if err != nil {
			return nil, err
		}

		values[i] = value
	}

	return &structpb.ListValue{
		Values: values,
	}, nil
}

func convertValue(path string, v interface{}) (*structpb.Value, error) {
	switch v := v.(type) {
	case string:
		return &structpb.Value{
			Kind: &structpb.Value_StringValue{
				StringValue: v,
			},
		}, nil
	case int:
		return &structpb.Value{
			Kind: &structpb.Value_NumberValue{
				NumberValue: float64(v),
			},
		}, nil
	case float64:
		return &structpb.Value{
			Kind: &structpb.Value_NumberValue{
				NumberValue: v,
			},
		}, nil
	case bool:
		return &structpb.Value{
			Kind: &structpb.Value_BoolValue{
				BoolValue: v,
			},
		}, nil
	case nil:
		return nil, nil
	case map[string]interface{}:
		objVal, err := convertObject(path, v)
		if err != nil {
			return nil, err
		}
		return &structpb.Value{
			Kind: &structpb.Value_StructValue{
				StructValue: objVal,
			},
		}, nil
	case []interface{}:
		lVal, err := convertList(path, v)
		if err != nil {
			return nil, err
		}
		return &structpb.Value{
			Kind: &structpb.Value_ListValue{
				ListValue: lVal,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported type %T for key `%s`", v, path)
	}
}

var _ yaml.BytesUnmarshaler = (*ProtoStruct)(nil)
