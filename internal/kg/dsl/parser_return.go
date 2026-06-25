package dsl

import (
	"fmt"
	"strings"
)

// parseReturn parses `RETURN item (, item)*`. Each item is a field projection,
// an intrinsic (hop_count), or an allowed-function call (count/min/max/
// coalesce). `RETURN p` where p is a path variable is rejected at validation
// (T31) because path variables never bind as aliases.
func (p *parser) parseReturn() error {
	p.next() // RETURN
	for {
		item, err := p.parseReturnItem()
		if err != nil {
			return err
		}
		p.q.Returns = append(p.q.Returns, item)
		if p.isPunct(",") {
			p.next()
			continue
		}
		return nil
	}
}

// parseReturnItem parses a single RETURN element and its optional `AS alias`.
func (p *parser) parseReturnItem() (ReturnItem, error) {
	item, err := p.parseReturnCore()
	if err != nil {
		return ReturnItem{}, err
	}
	if p.isIdent("AS") {
		p.next()
		alias, err := p.expectIdent()
		if err != nil {
			return ReturnItem{}, err
		}
		item.Alias = alias
	} else if item.Alias == "" {
		item.Alias = defaultReturnAlias(item)
	}
	return item, nil
}

// parseReturnCore parses the value half of a RETURN item: a function call
// (hop_count, count(*), min/max/coalesce(...)) or a plain field reference.
func (p *parser) parseReturnCore() (ReturnItem, error) {
	t := p.cur()
	if t.kind == tokIdent && t.text == funcHopCount {
		p.next()
		return ReturnItem{Func: funcHopCount}, nil
	}
	if t.kind == tokIdent && p.toks[p.pos+1].text == "(" && allowedReturnFuncs[t.text] {
		return p.parseReturnFunc()
	}
	if t.kind == tokParam {
		// Bare param projection, e.g. the none adapter's `RETURN $changed_ids`.
		// The param name lives in FuncArgs[0].Alias so an explicit AS can still
		// rename the output column without losing which param to read.
		p.next()
		return ReturnItem{
			Func:     returnParamFunc,
			Alias:    "$" + t.text,
			FuncArgs: []ReturnItem{{Alias: t.text}},
		}, nil
	}
	ref, err := p.parseFieldRef()
	if err != nil {
		return ReturnItem{}, err
	}
	return ReturnItem{Ref: ref}, nil
}

// parseReturnFunc parses an aggregate/normalizing function in RETURN:
// count(*), min(field), max(field), or coalesce(args...).
func (p *parser) parseReturnFunc() (ReturnItem, error) {
	name := p.next().text
	if err := p.expectPunct("("); err != nil {
		return ReturnItem{}, err
	}
	if name == funcCount {
		return p.parseCountStar(name)
	}
	args, err := p.parseReturnFuncArgs()
	if err != nil {
		return ReturnItem{}, err
	}
	return ReturnItem{Func: name, FuncArgs: args}, nil
}

// parseCountStar parses the `count(*)` form (the only count shape in v1).
func (p *parser) parseCountStar(name string) (ReturnItem, error) {
	if err := p.expectPunct("*"); err != nil {
		return ReturnItem{}, fmt.Errorf("dsl: only count(*) is supported in v1: %w", err)
	}
	if err := p.expectPunct(")"); err != nil {
		return ReturnItem{}, err
	}
	return ReturnItem{Func: name}, nil
}

// parseReturnFuncArgs parses the comma-separated args of min/max/coalesce in
// RETURN. Each arg is a field reference or a value expression (params/literals).
func (p *parser) parseReturnFuncArgs() ([]ReturnItem, error) {
	var args []ReturnItem
	for {
		arg, err := p.parseReturnArg()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.isPunct(",") {
			p.next()
			continue
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		return args, nil
	}
}

// parseReturnArg parses one argument of a RETURN function: a $param/literal
// (coalesce default) or a field reference (min/max/coalesce source).
func (p *parser) parseReturnArg() (ReturnItem, error) {
	t := p.cur()
	if t.kind == tokParam {
		p.next()
		return ReturnItem{Ref: FieldRef{}, Func: "", Alias: "$" + t.text}, nil
	}
	if t.kind == tokString || t.kind == tokNumber {
		p.next()
		return ReturnItem{Alias: t.text}, nil
	}
	ref, err := p.parseFieldRef()
	if err != nil {
		return ReturnItem{}, err
	}
	return ReturnItem{Ref: ref}, nil
}

// returnParamFunc marks a RETURN item that projects a bare query param (the
// none adapter's `RETURN $changed_ids`). The param name is stored in Alias as
// "$name" until an explicit AS overrides it.
const returnParamFunc = "@param"

// defaultReturnAlias derives the output column name for an item that omits AS,
// matching the source-text projection convention (e.g. c.id → "c.id",
// hop_count → "hop_count", count(*) → "count").
func defaultReturnAlias(item ReturnItem) string {
	if item.Func != "" && len(item.FuncArgs) == 0 {
		return item.Func
	}
	if item.Func != "" {
		return item.Func
	}
	return item.Ref.describe()
}

// isIDField reports whether the ref is exactly `alias.id`.
func (f FieldRef) isIDField() bool {
	return len(f.Path) == 1 && f.Path[0] == "id"
}

// describe renders the ref as its source text (`alias.part.part`) for errors
// and default column naming.
func (f FieldRef) describe() string {
	if len(f.Path) == 0 {
		return f.Alias
	}
	return f.Alias + "." + strings.Join(f.Path, ".")
}
