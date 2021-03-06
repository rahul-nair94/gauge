// Copyright 2015 ThoughtWorks, Inc.

// This file is part of Gauge.

// Gauge is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// Gauge is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with Gauge.  If not, see <http://www.gnu.org/licenses/>.

package conceptExtractor

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"path"

	"github.com/getgauge/common"
	"github.com/getgauge/gauge/formatter"
	"github.com/getgauge/gauge/gauge"
	"github.com/getgauge/gauge/gauge_messages"
	"github.com/getgauge/gauge/parser"
	"github.com/getgauge/gauge/util"
)

const (
	SPEC_HEADING_TEMPLATE = "# S\n\n"
	TABLE                 = "table"
)

type extractor struct {
	conceptName    string
	conceptStep    *gauge.Step
	stepsToExtract []*gauge_messages.Step
	stepsInConcept string
	table          *gauge.Table
	fileContent    string
	dynamicArgs    []string
	errors         []error
}

func ExtractConcept(conceptName *gauge_messages.Step, steps []*gauge_messages.Step, conceptFileName string, changeAcrossProject bool, selectedTextInfo *gauge_messages.TextInfo) (bool, error, []string) {
	content := SPEC_HEADING_TEMPLATE
	if util.IsSpec(selectedTextInfo.GetFileName()) {
		content, _ = common.ReadFileContents(selectedTextInfo.GetFileName())
	}
	concept, conceptUsageText, err := getExtractedConcept(conceptName, steps, content, selectedTextInfo.GetFileName())
	if err != nil {
		return false, err, []string{}
	}
	writeConceptToFile(concept, conceptUsageText, conceptFileName, selectedTextInfo.GetFileName(), selectedTextInfo)
	return true, errors.New(""), []string{conceptFileName, selectedTextInfo.GetFileName()}
}

func ReplaceExtractedStepsWithConcept(selectedTextInfo *gauge_messages.TextInfo, conceptText string) string {
	content, _ := common.ReadFileContents(selectedTextInfo.GetFileName())
	return replaceText(content, selectedTextInfo, conceptText)
}

func replaceText(content string, info *gauge_messages.TextInfo, replacement string) string {
	parts := regexp.MustCompile("\r\n|\n").Split(content, -1)
	for i := info.GetStartingLineNo(); i < info.GetEndLineNo(); i++ {
		parts = append(parts[:info.GetStartingLineNo()], parts[info.GetStartingLineNo()+1:]...)
	}
	parts[info.GetStartingLineNo()-1] = replacement
	return strings.Join(parts, "\n")
}

func writeConceptToFile(concept string, conceptUsageText string, conceptFileName string, fileName string, info *gauge_messages.TextInfo) {
	if _, err := os.Stat(conceptFileName); os.IsNotExist(err) {
		basepath := path.Dir(conceptFileName)
		if _, err := os.Stat(basepath); os.IsNotExist(err) {
			os.MkdirAll(basepath, common.NewDirectoryPermissions)
		}
		os.Create(conceptFileName)
	}
	content, _ := common.ReadFileContents(conceptFileName)
	util.SaveFile(conceptFileName, content+"\n"+concept, true)
	text := ReplaceExtractedStepsWithConcept(info, conceptUsageText)
	util.SaveFile(fileName, text, true)
}

func getExtractedConcept(conceptName *gauge_messages.Step, steps []*gauge_messages.Step, content string, cptFileName string) (string, string, error) {
	tokens, _ := new(parser.SpecParser).GenerateTokens("* "+conceptName.GetName(), cptFileName)
	conceptStep, _ := parser.CreateStepUsingLookup(tokens[0], nil, cptFileName)
	cptDict, _ := parser.ParseConcepts()
	if isDuplicateConcept(conceptStep, cptDict) {
		return "", "", fmt.Errorf("Concept `%s` already present", conceptName.GetName())
	}
	specText, err := getContentWithDataTable(content, cptFileName)
	if err != nil {
		return "", "", err
	}
	extractor := &extractor{conceptName: "* " + conceptName.GetName(), stepsInConcept: "", stepsToExtract: steps, conceptStep: conceptStep, table: &gauge.Table{}, fileContent: specText, errors: make([]error, 0)}
	extractor.extractSteps(cptFileName)
	if len(extractor.errors) != 0 {
		return "", "", err
	}
	conceptStep.ReplaceArgsWithDynamic(conceptStep.Args)
	addArgsFromTable(conceptStep, &extractor.conceptName, extractor.dynamicArgs)
	if extractor.table.IsInitialized() {
		extractor.conceptName += "\n" + formatter.FormatTable(extractor.table)
	}
	return strings.Replace(formatter.FormatStep(conceptStep), "* ", "# ", 1) + (extractor.stepsInConcept), extractor.conceptName, nil
}

func addArgsFromTable(concept *gauge.Step, conceptName *string, args []string) {
	for _, arg := range args {
		concept.Value += " {}"
		concept.Args = append(concept.Args, &gauge.StepArg{Value: arg, ArgType: gauge.Dynamic, Name: arg})
		*conceptName += fmt.Sprintf(" <%s>", arg)
	}
}

func getContentWithDataTable(content, cptFileName string) (string, error) {
	spec, result := new(parser.SpecParser).Parse(content, &gauge.ConceptDictionary{}, cptFileName)
	if !result.Ok {
		return "", fmt.Errorf("Spec Parse failure: %s", result.ParseErrors)
	}
	newSpec := &gauge.Specification{Heading: &gauge.Heading{Value: "SPECHEADING"}}
	if spec.DataTable.IsInitialized() {
		newSpec = &gauge.Specification{Items: []gauge.Item{&spec.DataTable}, Heading: &gauge.Heading{Value: "SPECHEADING"}}
	}
	return formatter.FormatSpecification(newSpec) + "\n##hello \n* step \n", nil
}

func isDuplicateConcept(concept *gauge.Step, cptDict *gauge.ConceptDictionary) bool {
	for _, cpt := range cptDict.ConceptsMap {
		if strings.TrimSpace(cpt.ConceptStep.Value) == strings.TrimSpace(concept.Value) {
			return true
		}
	}
	return false
}

func (e *extractor) extractSteps(cptFileName string) {
	for _, step := range e.stepsToExtract {
		tokens, _ := new(parser.SpecParser).GenerateTokens("*"+step.GetName(), cptFileName)
		stepInConcept, _ := parser.CreateStepUsingLookup(tokens[0], nil, cptFileName)
		if step.GetTable() != "" {
			e.handleTable(stepInConcept, step, cptFileName)
		}
		stepInConcept.ReplaceArgsWithDynamic(e.conceptStep.Args)
		e.stepsInConcept += formatter.FormatStep(stepInConcept)
	}
}

func (e *extractor) handleTable(stepInConcept *gauge.Step, step *gauge_messages.Step, cptFileName string) {
	stepInConcept.Value += " {}"
	specText := e.fileContent + step.GetTable()
	spec, result := new(parser.SpecParser).Parse(specText, &gauge.ConceptDictionary{}, cptFileName)
	if !result.Ok {
		for _, err := range result.ParseErrors {
			e.errors = append(e.errors, err)
		}
		return
	}
	stepArgs := []*gauge.StepArg{spec.Scenarios[0].Steps[0].Args[0]}
	e.addTableAsParam(step, stepArgs)
	stepInConcept.Args = append(stepInConcept.Args, stepArgs[0])
}

func (e *extractor) addTableAsParam(step *gauge_messages.Step, args []*gauge.StepArg) {
	if step.GetParamTableName() != "" {
		e.conceptName = strings.Replace(e.conceptName, fmt.Sprintf("<%s>", step.GetParamTableName()), "", 1)
		e.table = &args[0].Table
		args[0] = &gauge.StepArg{Value: step.GetParamTableName(), ArgType: gauge.Dynamic}
	} else {
		e.dynamicArgs = append(e.dynamicArgs, (&args[0].Table).GetDynamicArgs()...)
	}
}
