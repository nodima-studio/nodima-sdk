package v1

import (
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// BatchToArrow converts the transitional Nodima batch model to an Arrow
// record batch. Callers own the returned record and must Release it.
func BatchToArrow(batch Batch) (arrow.RecordBatch, error) {
	if err := batch.Validate(); err != nil {
		return nil, err
	}

	fields := make([]arrow.Field, len(batch.Columns))
	for index, column := range batch.Columns {
		dataType, err := arrowType(column.Type)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", column.Name, err)
		}
		fields[index] = arrow.Field{
			Name:     column.Name,
			Type:     dataType,
			Nullable: len(column.Valid) > 0,
		}
	}

	builder := array.NewRecordBuilder(
		memory.DefaultAllocator,
		arrow.NewSchema(fields, nil),
	)
	defer builder.Release()

	for index, column := range batch.Columns {
		valid := column.Valid
		switch column.Type {
		case DataTypeBoolean:
			builder.Field(index).(*array.BooleanBuilder).AppendValues(column.Boolean, valid)
		case DataTypeInt64:
			builder.Field(index).(*array.Int64Builder).AppendValues(column.Int64, valid)
		case DataTypeFloat64:
			builder.Field(index).(*array.Float64Builder).AppendValues(column.Float64, valid)
		case DataTypeString:
			builder.Field(index).(*array.StringBuilder).AppendValues(column.String, valid)
		case DataTypeBytes:
			builder.Field(index).(*array.BinaryBuilder).AppendValues(column.Bytes, valid)
		default:
			return nil, fmt.Errorf("column %q has unsupported data type %q", column.Name, column.Type)
		}
	}

	return builder.NewRecordBatch(), nil
}

// ArrowToBatch converts the current portable Arrow subset to Nodima's
// transitional in-process batch model. The returned values do not retain
// references to the Arrow record's buffers.
func ArrowToBatch(record arrow.RecordBatch) (Batch, error) {
	if err := ValidateArrowRecord(record); err != nil {
		return Batch{}, err
	}

	result := Batch{
		RowCount: uint32(record.NumRows()),
		Columns:  make([]Column, record.NumCols()),
	}
	for index, field := range record.Schema().Fields() {
		column := Column{
			Name: field.Name,
		}
		values := record.Column(index)
		if values.NullN() > 0 {
			column.Valid = make([]bool, values.Len())
			for row := range values.Len() {
				column.Valid[row] = values.IsValid(row)
			}
		}

		switch typed := values.(type) {
		case *array.Boolean:
			column.Type = DataTypeBoolean
			column.Boolean = make([]bool, typed.Len())
			for row := range typed.Len() {
				if typed.IsValid(row) {
					column.Boolean[row] = typed.Value(row)
				}
			}
		case *array.Int64:
			column.Type = DataTypeInt64
			column.Int64 = make([]int64, typed.Len())
			for row := range typed.Len() {
				if typed.IsValid(row) {
					column.Int64[row] = typed.Value(row)
				}
			}
		case *array.Float64:
			column.Type = DataTypeFloat64
			column.Float64 = make([]float64, typed.Len())
			for row := range typed.Len() {
				if typed.IsValid(row) {
					column.Float64[row] = typed.Value(row)
				}
			}
		case *array.String:
			column.Type = DataTypeString
			column.String = make([]string, typed.Len())
			for row := range typed.Len() {
				if typed.IsValid(row) {
					column.String[row] = strings.Clone(typed.Value(row))
				}
			}
		case *array.Binary:
			column.Type = DataTypeBytes
			column.Bytes = make([][]byte, typed.Len())
			for row := range typed.Len() {
				if typed.IsValid(row) {
					column.Bytes[row] = append([]byte(nil), typed.Value(row)...)
				}
			}
		default:
			return Batch{}, fmt.Errorf("column %q has unsupported Arrow type %s", field.Name, field.Type)
		}
		result.Columns[index] = column
	}

	if err := result.Validate(); err != nil {
		return Batch{}, fmt.Errorf("converted Arrow batch is invalid: %w", err)
	}
	return result, nil
}

func ValidateArrowRecord(record arrow.RecordBatch) error {
	if record == nil {
		return fmt.Errorf("Arrow record batch is nil")
	}
	if record.NumRows() < 0 || uint64(record.NumRows()) > uint64(^uint32(0)) {
		return fmt.Errorf("Arrow row count %d exceeds Nodima batch limits", record.NumRows())
	}
	if record.NumCols() == 0 && record.NumRows() > 0 {
		return fmt.Errorf("non-empty Arrow batch requires at least one column")
	}

	names := make(map[string]struct{}, record.NumCols())
	for index, field := range record.Schema().Fields() {
		if field.Name == "" {
			return fmt.Errorf("Arrow column %d has no name", index)
		}
		if _, exists := names[field.Name]; exists {
			return fmt.Errorf("duplicate Arrow column name %q", field.Name)
		}
		names[field.Name] = struct{}{}

		values := record.Column(index)
		if int64(values.Len()) != record.NumRows() {
			return fmt.Errorf(
				"Arrow column %q length is %d, want %d",
				field.Name,
				values.Len(),
				record.NumRows(),
			)
		}
		if !field.Nullable && values.NullN() > 0 {
			return fmt.Errorf("non-nullable Arrow column %q contains nulls", field.Name)
		}
		switch values.(type) {
		case *array.Boolean, *array.Int64, *array.Float64, *array.String, *array.Binary:
		default:
			return fmt.Errorf("column %q has unsupported Arrow type %s", field.Name, field.Type)
		}
	}
	return nil
}

func arrowType(dataType DataType) (arrow.DataType, error) {
	switch dataType {
	case DataTypeBoolean:
		return arrow.FixedWidthTypes.Boolean, nil
	case DataTypeInt64:
		return arrow.PrimitiveTypes.Int64, nil
	case DataTypeFloat64:
		return arrow.PrimitiveTypes.Float64, nil
	case DataTypeString:
		return arrow.BinaryTypes.String, nil
	case DataTypeBytes:
		return arrow.BinaryTypes.Binary, nil
	default:
		return nil, fmt.Errorf("unsupported Nodima data type %q", dataType)
	}
}
