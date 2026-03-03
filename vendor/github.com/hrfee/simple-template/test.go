package simpletemplate

import (
	"strings"
	"testing"
)

type templatingFunc func(in string, vars, conds []string, vals map[string]any) (string, error)

func benchmarkBlankTemplate(b *testing.B, templateFunc templatingFunc) {
	in := `Success, user! Your account has been created. Log in at myAccountURL with your username to get started.`
	var vars, conds []string
	vals := map[string]any{}
	for b.Loop() {
		templateFunc(in, vars, conds, vals)
	}
}

func benchmarkConditional(isTrue bool, b *testing.B, templateFunc templatingFunc) {
	in := `Success, {username}! Your account has been created. {if myCondition}Log in at {myAccountURL} with username {username} to get started.{endif}`
	vars := []string{"username", "myAccountURL", "myCondition"}
	conds := vars
	vals := map[string]any{
		"username":     "TemplateUsername",
		"myAccountURL": "TemplateURL",
		"myCondition":  isTrue,
	}
	for b.Loop() {
		templateFunc(in, vars, conds, vals)
	}
}

func benchmarkConditionalTrue(b *testing.B, templateFunc templatingFunc) {
	benchmarkConditional(true, b, templateFunc)
}
func benchmarkConditionalFalse(b *testing.B, templateFunc templatingFunc) {
	benchmarkConditional(false, b, templateFunc)
}

// In == Out when nothing is meant to be templated.
func testBlankTemplate(t *testing.T, templateFunc templatingFunc) {
	in := `Success, user! Your account has been created. Log in at myAccountURL with your username to get started.`

	out, err := templateFunc(in, []string{}, []string{}, map[string]any{})

	if err != nil {
		t.Fatalf("error: %+v", err)
	}

	if out != in {
		t.Fatalf(`returned string doesn't match input: "%+v" != "%+v"`, out, in)
	}
}

func testConditional(isTrue bool, t *testing.T, templateFunc templatingFunc) {
	in := `Success, {username}! Your account has been created. {if myCondition}Log in at {myAccountURL} with username {username} to get started.{endif}`

	vars := []string{"username", "myAccountURL", "myCondition"}
	conds := vars
	vals := map[string]any{
		"username":     "TemplateUsername",
		"myAccountURL": "TemplateURL",
		"myCondition":  isTrue,
	}

	out, err := templateFunc(in, vars, conds, vals)

	target := ""
	if isTrue {
		target = `Success, {username}! Your account has been created. Log in at {myAccountURL} with username {username} to get started.`
	} else {
		target = `Success, {username}! Your account has been created. `
	}

	target = strings.ReplaceAll(target, "{username}", vals["username"].(string))
	target = strings.ReplaceAll(target, "{myAccountURL}", vals["myAccountURL"].(string))

	if err != nil {
		t.Fatalf("error: %+v", err)
	}

	if out != target {
		t.Fatalf(`returned string doesn't match desired output: "%+v" != "%+v"`, out, target)
	}
}

func testConditionalTrue(t *testing.T, templateFunc templatingFunc) {
	testConditional(true, t, templateFunc)
}

func testConditionalFalse(t *testing.T, templateFunc templatingFunc) {
	testConditional(false, t, templateFunc)
}

// Template mistakenly double-braced values, but return a warning.
func testTemplateDoubleBraceGracefulHandling(t *testing.T, templateFunc templatingFunc) {
	in := `Success, {{username}}! Your account has been created. Log in at {myAccountURL} with username {username} to get started.`

	vars := []string{"username", "myAccountURL"}
	vals := map[string]any{
		"username":     "TemplateUsername",
		"myAccountURL": "TemplateURL",
	}

	target := strings.ReplaceAll(in, "{{username}}", vals["username"].(string))
	target = strings.ReplaceAll(target, "{username}", vals["username"].(string))
	target = strings.ReplaceAll(target, "{myAccountURL}", vals["myAccountURL"].(string))

	out, err := templateFunc(in, vars, []string{}, vals)

	if err == nil {
		t.Fatal("no error when given double-braced variable")
	}

	if out != target {
		t.Fatalf(`returned string doesn't match desired output: "%+v" != "%+v"`, out, target)
	}
}

func testVarAtAnyPosition(t *testing.T, templateFunc templatingFunc) {
	in := `Success, user! Your account has been created. Log in at myAccountURL with your username to get started.`
	vars := []string{"username", "myAccountURL"}
	vals := map[string]any{
		"username":     "TemplateUsername",
		"myAccountURL": "TemplateURL",
	}

	for i := range in {
		newIn := in[0:i] + "{" + vars[0] + "}" + in[i:]

		target := strings.ReplaceAll(newIn, "{"+vars[0]+"}", vals["username"].(string))

		out, err := templateFunc(newIn, vars, []string{}, vals)

		if err != nil {
			t.Fatalf("error: %+v", err)
		}

		if out != target {
			t.Fatalf(`returned string doesn't match desired output: "%+v" != "%+v, from "%+v""`, out, target, newIn)
		}
	}
}

// In previous version, a lone { would be left alone but a warning would be returned.
// In new version, that's harder to implement so we'll return an error.
func testIncompleteBlock(t *testing.T, templateFunc templatingFunc) {
	in := `Success, user! Your account has been created. Log in at myAccountURL with your username to get started.`
	for i := range in {
		newIn := in[0:i] + "{" + in[i:]

		_, err := templateFunc(newIn, []string{"a"}, []string{"a"}, map[string]any{"a": "a"})

		// if out != newIn {
		// 	t.Fatalf(`returned string for position %d/%d doesn't match desired output: "%+v" != "%+v", err=%v`, i+1, len(newIn), out, newIn, err)
		// }
		if err == nil {
			t.Fatalf("no error when given incomplete block with brace at position %d/%d", i+1, len(newIn))
		}

	}
}

func testNegation(t *testing.T, templateFunc templatingFunc) {
	in := `Success, {username}! Your account has been created. {if !myCondition}Log in at {myAccountURL} with username {username} to get started.{endif}`

	vars := []string{"username", "myAccountURL", "myCondition"}
	conds := vars
	vals := map[string]any{
		"username":     "TemplateUsername",
		"myAccountURL": "TemplateURL",
	}

	f := func(isTrue bool, t *testing.T) {
		out, err := templateFunc(in, vars, conds, vals)

		target := ""
		if isTrue {
			target = `Success, {username}! Your account has been created. Log in at {myAccountURL} with username {username} to get started.`
		} else {
			target = `Success, {username}! Your account has been created. `
		}

		target = strings.ReplaceAll(target, "{username}", vals["username"].(string))
		target = strings.ReplaceAll(target, "{myAccountURL}", vals["myAccountURL"].(string))

		if err != nil {
			t.Fatalf("error: %+v", err)
		}

		if out != target {
			t.Fatalf(`returned string doesn't match desired output: "%+v" != "%+v"`, out, target)
		}
	}

	t.Run("unassigned,true", func(t *testing.T) {
		f(true, t)
	})
	vals["myCondition"] = ""
	t.Run("blank,true", func(t *testing.T) {
		f(true, t)
	})
	vals["myCondition"] = "nonEmptyValue"
	t.Run("false", func(t *testing.T) {
		f(false, t)
	})
}

func testNestedIf(t *testing.T, templateFunc templatingFunc) {
	in := `{if varA}a{if varB}b{endif}{endif}`
	cases := []struct {
		name   string
		a, b   bool
		target string
	}{
		{"ff", false, false, ""},
		{"ft", false, true, ""},
		{"tf", true, false, "a"},
		{"tt", true, true, "ab"},
	}
	vars := []string{"varA", "varB"}
	conds := vars
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			vals := map[string]any{
				"varA": testCase.a,
				"varB": testCase.b,
			}
			out, err := templateFunc(in, vars, conds, vals)
			if err != nil {
				t.Fatalf("error: %+v", err)
			}
			if out != testCase.target {
				t.Fatalf(`returned string doesn't match desired output: "%+v" != "%+v"`, out, testCase.target)
			}
		})
	}
}
