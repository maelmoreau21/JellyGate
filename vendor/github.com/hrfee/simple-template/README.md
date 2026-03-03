# simple-template
[![Go Reference](https://pkg.go.dev/badge/github.com/hrfee/simple-template.svg)](https://pkg.go.dev/github.com/hrfee/simple-template)

simple templater for templates written by an end user. see godoc for more info.

## todo

- [ ] Implement else/else if
- [ ] Duplicate implementation in Typescript

## rough perf comparison

just for fun, not scientific. by suffix:
* "": This library
* "Old": Old version (-tags oldimpl)
* "BuiltinOnDemand": text/template with equivalent logic, template compile time included (-tags comparebuiltin)
* "BuiltinPrecompiled": text/template with equivalent logic, template compile time excluded (-tags comparebuiltin)
```
```
goos: linux
goarch: amd64
pkg: github.com/hrfee/simple-template
cpu: Intel(R) Core(TM) Ultra 9 185H
BenchmarkBlankTemplateBuiltinOnDemand-22          	1017019	     1169 ns/op
BenchmarkConditionalTrueBuiltinOnDemand-22        	 193219	     6022 ns/op
BenchmarkConditionalFalseBuiltinOnDemand-22       	 190468	     6045 ns/op
BenchmarkBlankTemplateBuiltinPrecompiled-22       	10806879	      105.6 ns/op
BenchmarkConditionalTrueBuiltinPrecompiled-22     	 925048	     1081 ns/op
BenchmarkConditionalFalseBuiltinPrecompiled-22    	 743378	     1389 ns/op
BenchmarkBlankTemplateOld-22                      	8661529	      138.0 ns/op
BenchmarkConditionalTrueOld-22                    	1698522	      706.5 ns/op
BenchmarkConditionalFalseOld-22                   	3465555	      343.9 ns/op
BenchmarkBlankTemplate-22                         	4308925	      285.0 ns/op
BenchmarkConditionalTrue-22                       	1549011	      765.8 ns/op
BenchmarkConditionalFalse-22                      	2028720	      595.1 ns/op
PASS
ok  	github.com/hrfee/simple-template	13.895s
```
