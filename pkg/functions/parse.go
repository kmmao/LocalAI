package functions

import (
	"encoding/json"
	"regexp"

	"github.com/go-skynet/LocalAI/pkg/utils"
	"github.com/rs/zerolog/log"
)

type FunctionsConfig struct {
	DisableNoAction         bool   `yaml:"disable_no_action"`
	NoActionFunctionName    string `yaml:"no_action_function_name"`
	NoActionDescriptionName string `yaml:"no_action_description_name"`
	ParallelCalls           bool   `yaml:"parallel_calls"`
	NoGrammar               bool   `yaml:"no_grammar"`
	ResponseRegex           string `yaml:"response_regex"`

	// FunctionName enable the LLM to return { "name": "function_name", "arguments": { "arg1": "value1", "arg2": "value2" } }
	// instead of { "function": "function_name", "arguments": { "arg1": "value1", "arg2": "value2" } }.
	// This might be useful for certain models trained with the function name as the first token.
	FunctionName bool `yaml:"return_name_in_function_response"`
}

type FuncCallResults struct {
	Name      string
	Arguments string
}

func ParseFunctionCall(llmresult string, functionConfig FunctionsConfig) []FuncCallResults {
	multipleResults := functionConfig.ParallelCalls
	useGrammars := !functionConfig.NoGrammar

	functionNameKey := "function"
	if functionConfig.FunctionName {
		functionNameKey = "name"
	}

	results := []FuncCallResults{}

	// if no grammar is used, we have to extract function and arguments from the result
	if !useGrammars {
		// the response is a string that we have to parse

		// We use named regexes here to extract the function name and arguments
		// obviously, this expects the LLM to be stable and return correctly formatted JSON
		// TODO: optimize this and pre-compile it
		var respRegex = regexp.MustCompile(functionConfig.ResponseRegex)
		match := respRegex.FindStringSubmatch(llmresult)
		result := make(map[string]string)
		for i, name := range respRegex.SubexpNames() {
			if i != 0 && name != "" && len(match) > i {
				result[name] = match[i]
			}
		}

		// TODO: open point about multiple results and/or mixed with chat messages
		// This is not handled as for now, we only expect one function call per response
		functionName := result[functionNameKey]
		if functionName == "" {
			return results
		}

		return append(results, FuncCallResults{Name: result[functionNameKey], Arguments: result["arguments"]})
	}

	// with grammars
	// TODO: use generics to avoid this code duplication
	if multipleResults {
		ss := []map[string]interface{}{}
		s := utils.EscapeNewLines(llmresult)
		err := json.Unmarshal([]byte(s), &ss)
		if err != nil {
			log.Error().Err(err).Str("escapedLLMResult", s).Msg("multiple results: unable to unmarshal llm result")
		}
		log.Debug().Msgf("Function return: %s %+v", s, ss)

		for _, s := range ss {
			func_name, ok := s[functionNameKey]
			if !ok {
				continue
			}
			args, ok := s["arguments"]
			if !ok {
				continue
			}
			d, _ := json.Marshal(args)
			funcName, ok := func_name.(string)
			if !ok {
				continue
			}
			results = append(results, FuncCallResults{Name: funcName, Arguments: string(d)})
		}
	} else {
		// As we have to change the result before processing, we can't stream the answer token-by-token (yet?)
		ss := map[string]interface{}{}
		// This prevent newlines to break JSON parsing for clients
		s := utils.EscapeNewLines(llmresult)
		err := json.Unmarshal([]byte(s), &ss)
		if err != nil {
			log.Error().Err(err).Str("escapedLLMResult", s).Msg("unable to unmarshal llm result")
		}
		log.Debug().Msgf("Function return: %s %+v", s, ss)

		// The grammar defines the function name as "function", while OpenAI returns "name"
		func_name, ok := ss[functionNameKey]
		if !ok {
			return results
		}
		// Similarly, while here arguments is a map[string]interface{}, OpenAI actually want a stringified object
		args, ok := ss["arguments"] // arguments needs to be a string, but we return an object from the grammar result (TODO: fix)
		if !ok {
			return results
		}
		d, _ := json.Marshal(args)
		funcName, ok := func_name.(string)
		if !ok {
			return results
		}
		results = append(results, FuncCallResults{Name: funcName, Arguments: string(d)})
	}

	return results
}
