package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"log"
	"sort"
	"strings"
)

func toCamelCase(snake string) string {
	parts := strings.Split(snake, "/")
	var res string
	for _, p := range parts {
		if p == "" {
			continue
		}
		p = strings.ReplaceAll(p, ":", "")
		p = strings.ReplaceAll(p, "-", "_")
		subparts := strings.Split(p, "_")
		for _, sp := range subparts {
			if len(sp) > 0 {
				res += strings.ToUpper(sp[:1]) + sp[1:]
			}
		}
	}
	return res
}

type replacement struct {
	start   int
	end     int
	newText string
	funcDef string
}

func main() {
	fset := token.NewFileSet()
	filePath := "../main.go"

	src, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatal(err)
	}

	f, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	var replacements []replacement
	funcNames := make(map[string]int)

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		methodName := sel.Sel.Name
		if methodName != "GET" && methodName != "POST" && methodName != "PUT" && methodName != "DELETE" && methodName != "PATCH" {
			return true
		}

		if len(call.Args) != 2 {
			return true
		}

		routeLit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || routeLit.Kind != token.STRING {
			return true
		}

		route := strings.Trim(routeLit.Value, "\"")

		funcLit, ok := call.Args[1].(*ast.FuncLit)
		if !ok {
			return true
		}

		if len(funcLit.Type.Params.List) != 1 || len(funcLit.Type.Results.List) != 1 {
			return true
		}

		paramType, ok := funcLit.Type.Params.List[0].Type.(*ast.SelectorExpr)
		if !ok || paramType.X.(*ast.Ident).Name != "echo" || paramType.Sel.Name != "Context" {
			return true
		}

		baseFuncName := "handle" + methodName + toCamelCase(route)
		funcName := baseFuncName
		if count := funcNames[baseFuncName]; count > 0 {
			funcName = fmt.Sprintf("%s%d", baseFuncName, count+1)
		}
		funcNames[baseFuncName]++

		startPos := fset.Position(funcLit.Pos()).Offset
		endPos := fset.Position(funcLit.End()).Offset

		body := string(src[startPos:endPos])

		usesCli := strings.Contains(body, "cli.") || strings.Contains(body, "cli,") || strings.Contains(body, "cli)") || strings.Contains(body, " cli ")
		usesAlertMgr := strings.Contains(body, "alertMgr")

		var replacementStr string
		var funcDefStr string

		args := []string{}
		params := []string{}

		if usesCli {
			args = append(args, "cli")
			params = append(params, "cli *client.Client")
		}
		if usesAlertMgr {
			args = append(args, "alertMgr")
			params = append(params, "alertMgr *alerts.AlertManager")
		}

		replacementStr = fmt.Sprintf("%s(%s)", funcName, strings.Join(args, ", "))
		funcDefStr = fmt.Sprintf("func %s(%s) echo.HandlerFunc {\n\treturn %s\n}", funcName, strings.Join(params, ", "), body)

		replacements = append(replacements, replacement{
			start:   startPos,
			end:     endPos,
			newText: replacementStr,
			funcDef: funcDefStr,
		})

		return true
	})

	var finalBuf bytes.Buffer
	lastIdx := 0

	sort.Slice(replacements, func(i, j int) bool {
		return replacements[i].start < replacements[j].start
	})

	for _, r := range replacements {
		finalBuf.Write(src[lastIdx:r.start])
		finalBuf.WriteString(r.newText)
		lastIdx = r.end
	}
	finalBuf.Write(src[lastIdx:])

	finalBuf.WriteString("\n\n")
	for _, def := range replacements {
		finalBuf.WriteString(def.funcDef)
		finalBuf.WriteString("\n\n")
	}

	if err := ioutil.WriteFile(filePath, finalBuf.Bytes(), 0644); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Done")
}
