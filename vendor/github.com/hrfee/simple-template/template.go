// Package simpletemplate provides a basic templater function which processes a simple syntax, intended to be exposed to an end user.
// For syntax see the example. The parser will also accept double braces (i.e. {{...}}) and single equals ({{ if x = y }}),
// but will return an error as a warning.
// It does not support nested if statements currently.
package simpletemplate

import (
	"bytes"
	"fmt"
)

// BlockType is the type of a parsed block.
type BlockType = int

const (
	PlainText  BlockType = iota // Plain text
	LogicOpen                   // { (or {{)
	LogicClose                  // } (or }})
	Word                        // A single word (only parsed like this when within braces)
	String                      // Plain text within quotes (only parsed like this when within braces). Quotes can be ', ", or `.
	EOF                         // End of file.
)

const (
	seekBufferSize = 3
)

func truthy(val interface{}) bool {
	switch v := val.(type) {
	case string:
		return v != ""
	case bool:
		return v
	case int:
		return v != 0
	}
	return false
}

type block struct {
	parent *templater
	a, b   int // start/end indices (inclusive)
	Type   BlockType
}

func (blk *block) String() string {
	if blk.Type == String {
		return blk.parent.input[blk.a+1 : blk.b]
	} else if blk.Type == EOF {
		return ""
	}
	return blk.parent.input[blk.a : blk.b+1]
}

func (blk *block) Describe() string {
	return fmt.Sprintf("%s[%d:%d]=\"%s\"", blockTypeToString(blk.Type), blk.a, blk.b, blk.String())
}

func blockTypeToString(b BlockType) string {
	switch b {
	case PlainText:
		return "PlainText"
	case LogicOpen:
		return "LogicOpen"
	case LogicClose:
		return "LogicClose"
	case Word:
		return "Word"
	case String:
		return "String"
	case EOF:
		return "EOF"
	}
	return "?"
}

func (b *block) expected(expected ...BlockType) error {
	return ExpectedTypeError{b.b, b.Type, expected}
}

func (b *block) expectedWord(expected string) error {
	return ExpectedError{b.b, b.String(), expected}
}

// DoubleBraceError indicates double braces were used instead of single braces. This being returned does not indicate that templating failed.
type DoubleBraceError struct{ pos int }

func (e DoubleBraceError) Error() string {
	return fmt.Sprintf(`double braces ("{{"/"}}") near char %d, use single braces only.`, e.pos)
}

// SingleEqualsError indicates a single equals sign ("=") was used in a comparison rather than two ("=="). This being returned does not indicate that templating failed.
type SingleEqualsError struct{ pos int }

func (e SingleEqualsError) Error() string {
	return fmt.Sprintf(`single equals ("=") used in if block near char %d, use double equals ("==").`, e.pos)
}

// ExpectedTypeError indicates the wrong block type was found at the position.
type ExpectedTypeError struct {
	Pos      int
	Got      BlockType
	Expected []BlockType // Expected one of these
}

func (e ExpectedTypeError) Error() string {
	expectedString := ""
	for i, bt := range e.Expected {
		expectedString += blockTypeToString(bt)
		if i != len(e.Expected)-1 {
			expectedString += " or "
		}
	}

	return fmt.Sprintf("near char %d: got type %s, expected %s", e.Pos, blockTypeToString(e.Got), expectedString)
}

// ExpectedError indicates the wrong text or character was found at the position.
type ExpectedError struct {
	Pos           int
	got, expected string
}

func (e ExpectedError) Error() string {
	return fmt.Sprintf("near char %d: got \"%s\", expected %s", e.Pos, e.got, e.expected)
}

type templater struct {
	input  string
	output bytes.Buffer
	len    int
	// Last read byte (i.e. start at -1)
	pos  int
	vals map[string]any
	// Flag set when we're in a { ... } (or {{ ... }}) block, indicating we should tokenize text.
	inLogic bool
	// Flag set when we're in a quoted string with the flag byte, or 0 when not.
	inString byte
	buffer   struct {
		buf [seekBufferSize]block
		pos int
	}
	warning error // Non-fatal error, returned at completion, rather than terminating early.
}

// Template completes the given template string given the values provided.
// If failed, will return an empty string and an error.
// If succeeded, will return the templated string and nil.
// If succeeded with a warning, will return the templated string and an error.
func Template(input string, vals map[string]any) (string, error) {
	t := templater{
		input:    input,
		len:      len(input),
		pos:      -1,
		vals:     vals,
		inLogic:  false,
		inString: 0,
		warning:  nil,
	}
	if vals == nil {
		t.vals = map[string]any{}
	}
	t.output.Grow(t.len)
	t.buffer.pos = 0
	for i := range seekBufferSize {
		t.next(&(t.buffer.buf[i]))
	}

	var a block
	var err error = nil
	a.Type = EOF
	for {
		a = t.nextFromBuf()
		if a.Type == EOF {
			break
		}
		err = t.process(&a, &(t.output))
		if err != nil {
			break
		}
	}
	if err == nil {
		err = t.warning
	}
	return t.output.String(), err
}

func (t *templater) getChar() byte {
	if t.pos+1 == t.len {
		return 0
	}
	t.pos += 1
	return t.input[t.pos]
}

func (t *templater) peekChar() byte {
	if t.pos+1 == t.len {
		return 0
	}
	return t.input[t.pos+1]
}

func (t *templater) next(blk *block) {
	blk.parent = t
	blk.Type = PlainText
	blk.a = t.pos + 1
	blk.b = -1
	var c byte
	for {
		c = t.getChar()
		if c == 0 {
			blk.Type = EOF
			break
		}
		if !t.inLogic {
			if c == '{' {
				blk.Type = LogicOpen
				blk.a = t.pos
				blk.b = t.pos
				if t.peekChar() == '{' {
					t.warning = DoubleBraceError{t.pos}
					t.getChar()
					blk.b = t.pos
				}
				t.inLogic = true
				break
			}
			blk.b = t.pos
			if t.peekChar() == '{' || t.peekChar() == 0 {
				break
			}
			continue
		}
		if t.inString != 0 {
			if c == t.inString {
				blk.b = t.pos
				t.inString = 0
				break
			} else {
				continue
			}
		}
		if c == '}' {
			blk.Type = LogicClose
			blk.a = t.pos
			blk.b = t.pos
			if t.peekChar() == '}' {
				t.warning = DoubleBraceError{t.pos}
				t.getChar()
				blk.b = t.pos
			}
			t.inLogic = false
			break
		}
		if c == '"' || c == '\'' || c == '`' {
			blk.Type = String
			blk.a = t.pos
			t.inString = c
			continue
		}
		if c == ' ' || c == '\t' {
			continue
		}
		if blk.Type != Word {
			blk.Type = Word
			blk.a = t.pos
		}
		if blk.Type == Word {
			blk.b = t.pos
			next := t.peekChar()
			if next == ' ' || next == '\t' || next == '}' {
				break
			}
		}
	}
}

func (t *templater) nextFromBuf() block {
	out := t.buffer.buf[t.buffer.pos]
	t.next(&(t.buffer.buf[t.buffer.pos]))
	t.buffer.pos = (t.buffer.pos + 1) % seekBufferSize
	// _, file, no, ok := runtime.Caller(1)
	// if ok {
	// 	fmt.Printf("called from %s#%d: %s\n", file, no, out.Describe())
	// }
	return out
}

func (t *templater) peek() block {
	return t.buffer.buf[t.buffer.pos]
}

func (t *templater) process(a *block, output *bytes.Buffer) error {
	switch a.Type {
	case PlainText:
		if output != nil {
			output.WriteString(a.String())
		}
		return nil
	case LogicOpen:
		return t.logicOpen(a, output)
	// LogicClose and Word/String should only occur within logic blocks and so
	// they should not appear here.
	case LogicClose:
	case Word:
	case String:
		return a.expected(LogicOpen, PlainText)
	}
	return nil
}

func (t *templater) processIfBody(ifTrue bool, output *bytes.Buffer) error {
	var next block
	var content bytes.Buffer
	var contentPtr *bytes.Buffer = nil
	if ifTrue {
		contentPtr = &content
	}
	for {
		next = t.nextFromBuf()
		if next.Type == EOF {
			break
		}
		if next.Type == LogicOpen {
			endif := t.peek()
			if endif.Type == Word {
				endifString := endif.String()
				if endifString == "endif" {
					t.nextFromBuf()
					shouldBeClose := t.nextFromBuf()
					if shouldBeClose.Type == LogicClose {
						if output != nil {
							output.Write(content.Bytes())
						}
						break
					}
				} else if endifString == "else" {
					t.nextFromBuf()
					shouldBeClose := t.nextFromBuf()
					// Invert if condition to decide if we evaluate the next else/else if body.
					if ifTrue {
						contentPtr = nil
					} else {
						contentPtr = &content
					}
					if shouldBeClose.Type == LogicClose {
						// Continue the loop, i.e. print/not print depending on inverted ifTrue.
						continue
					} else if shouldBeClose.String() == "if" {
						// Evaluate the if statement, let it calls its own copy of us.
						err := t.ifStatement(&shouldBeClose, contentPtr)
						if output != nil {
							output.Write(content.Bytes())
						}
						return err
					}
				}
			}
		}
		// We need to process for the sake of nested if statements.
		t.process(&next, contentPtr)
	}
	if next.Type == EOF {
		return next.expectedWord("{endif}")
	}
	return nil
}

func (t *templater) logicOpen(open *block, output *bytes.Buffer) error {
	ifWordOrVar := t.nextFromBuf()
	if ifWordOrVar.Type != Word {
		return ifWordOrVar.expected(Word)
	}

	closeOrOperand := t.peek()
	if closeOrOperand.Type == LogicClose {
		return t.templateValue(open, &ifWordOrVar, output)
	}
	return t.ifStatement(&ifWordOrVar, output)
}

func (t *templater) templateValue(open, variable *block, output *bytes.Buffer) error {
	if output != nil {
		val, ok := t.vals[variable.String()]
		if ok {
			fmt.Fprint(output, val)
		} else {
			// If var isn't found, leave output the same
			output.WriteString(open.String())
			output.WriteString(variable.String())
			close := t.nextFromBuf()
			output.WriteString(close.String())
		}
	}
	return nil
}

func (t *templater) ifStatement(ifWord *block, output *bytes.Buffer) error {
	if ifWord.String() != "if" {
		return ifWord.expectedWord("\"if\"")
	}

	operand := t.nextFromBuf()
	val1, err := t.operand(&operand)
	if err != nil {
		return err
	}

	comparisonOrClose := t.nextFromBuf()

	if comparisonOrClose.Type == LogicClose {
		return t.ifTruthy(&operand, val1, output)
	}
	return t.ifComparison(&operand, &comparisonOrClose, val1, output)
}

func (t *templater) ifTruthy(operand *block, val any, output *bytes.Buffer) error {
	// If Bool(val)
	positive := t.input[operand.a] != '!'
	return t.processIfBody(
		positive == truthy(val),
		output,
	)
}

func (t *templater) ifComparison(operandA, comparison *block, valA any, output *bytes.Buffer) error {
	// If valA ==/!= valB
	operandB := t.nextFromBuf()

	comparisonString := comparison.String()
	if comparisonString == "=" {
		t.warning = SingleEqualsError{comparison.a}
	} else if comparisonString != "==" && comparisonString != "!=" {
		return comparison.expectedWord("==/=/!=")
	}

	valB, err := t.operand(&operandB)
	if err != nil {
		return err
	}

	shouldBeClose := t.nextFromBuf()
	if shouldBeClose.Type != LogicClose {
		return shouldBeClose.expected(LogicClose)
	}

	var ifTrue bool
	if comparisonString == "==" || comparisonString == "=" {
		ifTrue = valA == valB
	} else if comparisonString == "!=" {
		ifTrue = valA != valB
	}
	return t.processIfBody(ifTrue, output)
}

func (t *templater) operand(a *block) (any, error) {
	if a.Type == String {
		return a.String(), nil
	} else if a.Type == Word {
		var name string
		if t.input[a.a] == '!' {
			name = t.input[a.a+1 : a.b+1]
		} else {
			name = a.String()
		}
		val, ok := t.vals[name]
		if ok {
			return val, nil
		} else {
			return "", nil
		}
	} else {
		return nil, a.expected(Word)
	}
}
