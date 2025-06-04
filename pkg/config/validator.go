package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-playground/validator/v10"
)

// ConfigValidator 配置校验器
type ConfigValidator struct {
	validator *validator.Validate
}

// NewConfigValidator 创建新的配置校验器
func NewConfigValidator() *ConfigValidator {
	v := validator.New()

	// 注册自定义校验规则
	_ = v.RegisterValidation("semver", validateSemver)

	// 使用 mapstructure 标签名作为字段名
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("mapstructure"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	return &ConfigValidator{
		validator: v,
	}
}

// Validate 验证配置结构体
func (cv *ConfigValidator) Validate(config any) error {
	if err := cv.validator.Struct(config); err != nil {
		// 转换校验错误为更友好的格式
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			var errorMessages []string
			for _, fieldError := range validationErrors {
				errorMessages = append(errorMessages, cv.formatFieldError(fieldError))
			}
			return fmt.Errorf("validation failed: %s", strings.Join(errorMessages, "; "))
		}
		return fmt.Errorf("validation error: %w", err)
	}
	return nil
}

// formatFieldError 格式化字段错误信息
func (cv *ConfigValidator) formatFieldError(fieldError validator.FieldError) string {
	fieldName := fieldError.Field()
	tag := fieldError.Tag()

	switch tag {
	case "required":
		return fmt.Sprintf("field '%s' is required", fieldName)
	case "min":
		return fmt.Sprintf("field '%s' must be at least %s", fieldName, fieldError.Param())
	case "max":
		return fmt.Sprintf("field '%s' must be at most %s", fieldName, fieldError.Param())
	case "oneof":
		return fmt.Sprintf("field '%s' must be one of: %s", fieldName, fieldError.Param())
	case "semver":
		return fmt.Sprintf("field '%s' must be a valid semantic version", fieldName)
	case "hostname_rfc1123":
		return fmt.Sprintf("field '%s' must be a valid hostname", fieldName)
	case "ip":
		return fmt.Sprintf("field '%s' must be a valid IP address", fieldName)
	case "uri":
		return fmt.Sprintf("field '%s' must be a valid URI", fieldName)
	default:
		return fmt.Sprintf("field '%s' validation failed: %s", fieldName, tag)
	}
}

// validateSemver 验证语义版本号
func validateSemver(fl validator.FieldLevel) bool {
	version := fl.Field().String()
	_, err := semver.NewVersion(version)
	return err == nil
}
