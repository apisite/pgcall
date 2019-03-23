// Copyright 2018 Aleksei Kovrizhkin <lekovr+apisite@gmail.com>. All rights reserved.

package pgcall

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/pkg/errors"
)

// Call postgresql stored function
func (srv *Server) Call(r *http.Request,
	method string,
	args map[string]interface{},
) (interface{}, error) {
	// Lookup method.
	methodSpec, ok := (*srv.Methods())[method]
	if !ok {
		return nil, (&CallError{code: NotFound}).addContext("name", method)
	}

	var inAssigns []string
	var missedArgs []string
	var inVars []interface{}

	if methodSpec.In != nil {
		srv.log.Debugf("IN args: %+v", *methodSpec.In)
		for k, v := range *methodSpec.In {
			a, ok := args[k]
			if !ok {
				if v.Required {
					missedArgs = append(missedArgs, k)
				} else {
					srv.log.Debugf("Skip missed value of %s", k)
				}
				continue
			}
			if reflect.ValueOf(a).Kind() == reflect.Ptr {
				if reflect.ValueOf(a).IsNil() {
					if v.Required {
						missedArgs = append(missedArgs, k)
					} else {
						srv.log.Debugf("Skip missed ref of %s", k)
					}
					continue
				}
				a = reflect.ValueOf(a).Elem().Interface() // dereference ptr
			}
			inAssigns = append(inAssigns, fmt.Sprintf("%s %s $%d", v.Name, srv.config.ArgSyntax, len(inAssigns)+1))
			inVars = append(inVars, a)
			srv.log.Debugf("Use: %s (%+v)", k, a)
		}
	}
	if len(missedArgs) > 0 {
		return nil, (&CallError{code: ArgsMissed}).addContext("args", missedArgs)
	}

	if methodSpec.Out == nil && methodSpec.Result == nil {
		// no data returned
		sql := fmt.Sprintf("SELECT %s.%s(%s)",
			methodSpec.Class,
			methodSpec.Func,
			strings.Join(inAssigns, ", "),
		)
		ctra, err := srv.dbh.Exec(sql, inVars...)
		srv.log.Debugf("Rows affected: %d", ctra) // TODO: Header.Add ?
		return nil, err
	}

	var outCols []string
	if methodSpec.Out != nil {
		for _, v := range *methodSpec.Out {
			outCols = append(outCols, v.Name)
		}
	}

	from := ""
	if len(outCols) > 0 {
		from = " from "
	}

	sql := fmt.Sprintf("select %s%s%s.%s(%s)",
		strings.Join(outCols, ", "),
		from,
		methodSpec.Class,
		methodSpec.Func,
		strings.Join(inAssigns, ", "),
	)
	srv.log.Debugf("sql: %s, args: %v\n", sql, inVars)
	var rv interface{}
	var err error
	if methodSpec.IsStruct {
		rv, err = srv.dbh.QueryMaps(sql, inVars...)
	} else {
		rv, err = srv.dbh.Query(sql, inVars...)
	}
	if err != nil {
		return nil, err
	}
	if !methodSpec.IsSet {
		rv1 := rv.([]interface{})
		if len(rv1) != 1 {
			return nil, errors.New("single row must be returned")
		}
		return &rv1[0], nil
	}
	return rv, nil
}
