// Copyright 2021-2024 Nokia
// Licensed under the BSD 3-Clause License.
// SPDX-License-Identifier: BSD-3-Clause

package restful

import (
	"context"
	"net/http"
	"reflect"

	"github.com/go-playground/validator/v10"
	"github.com/nokia/restful/lambda"
)

// LambdaMaxBytesToParse defines the maximum length of request content allowed to be parsed.
// If zero then no limits imposed.
var LambdaMaxBytesToParse = 0

// LambdaSanitizeJSON defines whether to sanitize JSON of Lambda return or SendResp.
// See SanitizeJSONString for details.
var LambdaSanitizeJSON = false

// LambdaValidator tells if incoming request is to be validated.
// Validation is done by https://github.com/go-playground/validator.
// See its documentation for details.
var LambdaValidator = true

// validate is a single instance, caching info about the struct to be validated.
var validate *validator.Validate = validator.New(validator.WithRequiredStructEnabled())

func lambdaHandleRes0(l *lambda.Lambda) (err error) {
	if l != nil && l.Status > 0 {
		err = NewError(nil, l.Status)
	}
	return
}

func lambdaHandleRes1(l *lambda.Lambda, res reflect.Value) (any, error) {
	if err, ok := res.Interface().(error); ok {
		return nil, err
	}

	if l != nil && l.Status > 0 {
		return res.Interface(), NewError(nil, l.Status)
	}
	return res.Interface(), nil
}

func lambdaGetStatus(l *lambda.Lambda, res reflect.Value) error {
	if err, ok := res.Interface().(error); ok {
		return err
	}
	if l != nil && l.Status > 0 {
		return NewError(nil, l.Status)
	}
	return nil
}

func lambdaHandleRes2(l *lambda.Lambda, res []reflect.Value) (any, error) {

	err := lambdaGetStatus(l, res[1])

	if res[0].Kind() == reflect.Ptr {
		if res[0].IsNil() {
			return nil, err
		}
		res[0] = res[0].Elem()
	}
	return res[0].Interface(), err
}

func lambdaHandleRes(w http.ResponseWriter, r *http.Request, res []reflect.Value) {
	var data any
	var err error
	if len(res) <= 0 {
		err = lambdaHandleRes0(L(r.Context()))
	} else if len(res) == 1 {
		data, err = lambdaHandleRes1(L(r.Context()), res[0])
	} else {
		data, err = lambdaHandleRes2(L(r.Context()), res)
	}
	_ = SendResp(w, r, err, data)
}

var contextType = reflect.TypeOf((*context.Context)(nil)).Elem()

func lambdaGetParams(w http.ResponseWriter, r *http.Request, f any) ([]reflect.Value, *http.Request, error) {
	t := reflect.TypeOf(f)
	params := make([]reflect.Value, t.NumIn())
	if t.NumIn() > 0 {
		reqDataIdx := 0

		// Handle context parameter
		if t.In(0).Implements(contextType) {
			ctx := NewRequestCtx(w, r)
			r = r.WithContext(ctx)
			params[0] = reflect.ValueOf(ctx)
			reqDataIdx = 1
		}

		// Handle body parameter
		if reqDataIdx < t.NumIn() {
			var reqData reflect.Value
			var reqDataInterface any
			reqDataType := t.In(reqDataIdx)
			if reqDataType.Kind() == reflect.Ptr {
				reqData = reflect.New(reqDataType.Elem())
				reqDataInterface = reqData.Interface()
			} else {
				reqData = reflect.New(reqDataType).Elem()
				reqDataInterface = reqData.Addr().Interface()
			}

			if err := GetRequestData(r, LambdaMaxBytesToParse, reqDataInterface); err != nil {
				return nil, r, err
			}

			if LambdaValidator && reflect.ValueOf(reqDataInterface).Elem().Kind() == reflect.Struct {
				if err := validate.Struct(reqDataInterface); err != nil {
					return nil, r, NewError(err, http.StatusUnprocessableEntity)
				}
			}

			params[reqDataIdx] = reqData
		}
	}
	return params, r, nil
}

// LambdaWrap wraps a Lambda function and makes it a http.HandlerFunc.
// This function is rarely needed, as restful's Router wraps handler functions automatically.
// You might need it if you want to wrap a standard http.HandlerFunc.
func LambdaWrap(f any) http.HandlerFunc {
	if httpHandler, ok := f.(func(w http.ResponseWriter, r *http.Request)); ok {
		return httpHandler
	}
	if httpHandler, ok := f.(http.HandlerFunc); ok {
		return httpHandler
	}

	t := reflect.TypeOf(f)
	if t.Kind() != reflect.Func {
		panic("function expected")
	}

	return func(w http.ResponseWriter, r *http.Request) {
		params, r, err := lambdaGetParams(w, r, f)
		if err != nil {
			_ = SendResp(w, r, err, nil)
			return
		}
		res := reflect.ValueOf(f).Call(params)
		lambdaHandleRes(w, r, res)
	}
}
