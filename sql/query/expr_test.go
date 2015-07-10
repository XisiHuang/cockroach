// Copyright 2015 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License. See the AUTHORS file
// for names of contributors.
//
// Author: Peter Mattis (peter@cockroachlabs.com)

package query

import (
	"testing"

	"github.com/cockroachdb/cockroach/sql/parser"
	"github.com/cockroachdb/cockroach/sql/sqlwire"
	"github.com/cockroachdb/cockroach/testutils"
)

func dInt(v int64) sqlwire.Datum {
	return sqlwire.Datum{IntVal: &v}
}

func dUint(v uint64) sqlwire.Datum {
	return sqlwire.Datum{UintVal: &v}
}

func dFloat(v float64) sqlwire.Datum {
	return sqlwire.Datum{FloatVal: &v}
}

func dBytes(v []byte) sqlwire.Datum {
	return sqlwire.Datum{BytesVal: v}
}

func dString(v string) sqlwire.Datum {
	return sqlwire.Datum{StringVal: &v}
}

func TestEvalExpr(t *testing.T) {
	testData := []struct {
		expr     string
		expected string
		env      Env
	}{
		// Bitwise operators.
		{`1 & 3`, `1`, nil},
		{`1 | 3`, `3`, nil},
		{`1 ^ 3`, `2`, nil},
		// Bitwise operators convert their arguments to ints.
		// TODO(pmattis): Should this be an error instead?
		{`1.1 ^ 3.1`, `2`, nil},
		// Arithmetic operators.
		{`1 + 1`, `2`, nil},
		{`1 - 2`, `-1`, nil},
		{`3 * 4`, `12`, nil},
		{`3.1 % 2.0`, `1.1`, nil},
		{`5 % 3`, `2`, nil},
		// Division is always done on floats.
		{`4 / 5`, `0.8`, nil},
		// Grouping
		{`1 + 2 + (3 * 4)`, `15`, nil},
		// Unary operators.
		{`-3`, `-3`, nil},
		{`-4.1`, `-4.1`, nil},
		// Ones complement operates on unsigned integers.
		{`~0`, `18446744073709551615`, nil},
		{`~0.1`, `18446744073709551615`, nil},
		{`~0 - 1`, `18446744073709551614`, nil},
		// Hexadecimal numbers.
		{`0xa`, `10`, nil},
		// Octal numbers.
		{`0755`, `493`, nil},
		// String conversion
		{`'1' + '2'`, `3`, nil},
		// Strings convert to floats.
		{`'18446744073709551614' + 1`, `1.8446744073709552e+19`, nil},
		// String concatenation.
		{`'a' || 'b'`, `ab`, nil},
		{`'a' || (1 + 2)`, `a3`, nil},
		// Column lookup.
		{`a`, `1`, mapEnv{"a": dInt(1)}},
		{`a`, `2`, mapEnv{"a": dUint(2)}},
		{`a`, `3.1`, mapEnv{"a": dFloat(3.1)}},
		{`a`, `b`, mapEnv{"a": dBytes([]byte("b"))}},
		{`a`, `c`, mapEnv{"a": dString("c")}},
		{`a.b + 1`, `2`, mapEnv{"a.b": dInt(1)}},
		// Boolean expressions.
		{`false AND true`, `false`, nil},
		{`true AND true`, `true`, nil},
		{`true AND false`, `false`, nil},
		{`false AND false`, `false`, nil},
		{`false OR true`, `true`, nil},
		{`true OR true`, `true`, nil},
		{`true OR false`, `true`, nil},
		{`false OR false`, `false`, nil},
		{`NOT false`, `true`, nil},
		{`NOT true`, `false`, nil},
		// Boolean expressions short-circuit the evaluation.
		{`false AND (a = 1)`, `false`, nil},
		{`true OR (a = 1)`, `true`, nil},
		// Comparisons.
		{`0 = 1`, `false`, nil},
		{`0 != 1`, `true`, nil},
		{`0 < 1`, `true`, nil},
		{`0 <= 1`, `true`, nil},
		{`0 > 1`, `false`, nil},
		{`0 >= 1`, `false`, nil},
		{`true = false`, `false`, nil},
		{`true != false`, `true`, nil},
		{`true < false`, `false`, nil},
		{`true <= false`, `false`, nil},
		{`true > false`, `true`, nil},
		{`true >= false`, `true`, nil},
		{`true <=> false`, `false`, nil},
		{`'a' = 'b'`, `false`, nil},
		{`'a' != 'b'`, `true`, nil},
		{`'a' < 'b'`, `true`, nil},
		{`'a' <= 'b'`, `true`, nil},
		{`'a' > 'b'`, `false`, nil},
		{`'a' >= 'b'`, `false`, nil},
		{`'a' >= 'b'`, `false`, nil},
		// Comparison of a string against a number compares using floating point.
		{`'10' > '2'`, `false`, nil},
		{`'10' > 2`, `true`, nil},
		// Comparisons against NULL result in NULL, except for the null-safe equal.
		{`0 = NULL`, `NULL`, nil},
		{`NULL = NULL`, `NULL`, nil},
		{`NULL <=> NULL`, `true`, nil},
		// NULL checks.
		{`0 IS NULL`, `false`, nil},
		{`0 IS NOT NULL`, `true`, nil},
		{`NULL IS NULL`, `true`, nil},
		{`NULL IS NOT NULL`, `false`, nil},
		// Range conditions.
		{`2 BETWEEN 1 AND 3`, `true`, nil},
		{`1 NOT BETWEEN 2 AND 3`, `true`, nil},
		{`'foo' BETWEEN 'a' AND 'z'`, `true`, nil},
		// Case operator.
		{`CASE WHEN true THEN 1 END`, `1`, nil},
		{`CASE WHEN false THEN 1 END`, `NULL`, nil},
		{`CASE WHEN false THEN 1 ELSE 2 END`, `2`, nil},
		{`CASE WHEN false THEN 1 WHEN false THEN 2 END`, `NULL`, nil},
	}
	for i, d := range testData {
		q, _, err := parser.Parse("SELECT " + d.expr)
		if err != nil {
			t.Fatalf("%d: %v: %s", i, err, d.expr)
		}
		expr := q.(*parser.Select).Exprs[0].(*parser.NonStarExpr).Expr
		r, err := EvalExpr(expr, d.env)
		if err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		if s := r.String(); d.expected != s {
			t.Errorf("%d: expected %s, but found %s: %s", i, d.expected, s, d.expr)
		}
	}
}

func TestEvalExprError(t *testing.T) {
	testData := []struct {
		expr     string
		expected string
	}{
		{`'a' + 0`, `parsing \"a\": invalid syntax`},
		{`'0a' + 0`, `parsing \"0a\": invalid syntax`},
		{`a`, `column \"a\" not found`},
		// TODO(pmattis): Check for overflow.
		// {`~0 + 1`, `0`, nil},
	}
	for i, d := range testData {
		q, _, err := parser.Parse("SELECT " + d.expr)
		if err != nil {
			t.Fatalf("%d: %v: %s", i, err, d.expr)
		}
		expr := q.(*parser.Select).Exprs[0].(*parser.NonStarExpr).Expr
		if _, err := EvalExpr(expr, mapEnv{}); !testutils.IsError(err, d.expected) {
			t.Errorf("%d: expected %s, but found %v", i, d.expected, err)
		}
	}
}
