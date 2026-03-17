package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	coreorchestration "github.com/opsorch/opsorch-core/orchestration"
	"github.com/opsorch/opsorch-core/schema"
	adapter "github.com/opsorch/opsorch-orchestration-adapter/orchestration"
)

type rpcRequest struct {
	Method  string          `json:"method"`
	Config  map[string]any  `json:"config"`
	Payload json.RawMessage `json:"payload"`
}

type rpcResponse struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

var provider coreorchestration.Provider

func main() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req rpcRequest
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			writeErr(enc, err)
			return
		}

		prov, err := ensureProvider(req.Config)
		if err != nil {
			writeErr(enc, err)
			continue
		}

		ctx := context.Background()
		switch req.Method {
		case "orchestration.plans.query":
			var query schema.OrchestrationPlanQuery
			if err := json.Unmarshal(req.Payload, &query); err != nil {
				writeErr(enc, err)
				continue
			}
			res, err := prov.QueryPlans(ctx, query)
			write(enc, res, err)
		case "orchestration.plans.get":
			var payload struct {
				PlanID string `json:"planId"`
			}
			if err := json.Unmarshal(req.Payload, &payload); err != nil {
				writeErr(enc, err)
				continue
			}
			res, err := prov.GetPlan(ctx, payload.PlanID)
			write(enc, res, err)
		case "orchestration.runs.query":
			var query schema.OrchestrationRunQuery
			if err := json.Unmarshal(req.Payload, &query); err != nil {
				writeErr(enc, err)
				continue
			}
			res, err := prov.QueryRuns(ctx, query)
			write(enc, res, err)
		case "orchestration.runs.get":
			var payload struct {
				RunID string `json:"runId"`
			}
			if err := json.Unmarshal(req.Payload, &payload); err != nil {
				writeErr(enc, err)
				continue
			}
			res, err := prov.GetRun(ctx, payload.RunID)
			write(enc, res, err)
		case "orchestration.runs.start":
			var payload struct {
				PlanID string `json:"planId"`
			}
			if err := json.Unmarshal(req.Payload, &payload); err != nil {
				writeErr(enc, err)
				continue
			}
			res, err := prov.StartRun(ctx, payload.PlanID)
			write(enc, res, err)
		case "orchestration.runs.steps.complete":
			var payload struct {
				RunID  string `json:"runId"`
				StepID string `json:"stepId"`
				Actor  string `json:"actor"`
				Note   string `json:"note"`
			}
			if err := json.Unmarshal(req.Payload, &payload); err != nil {
				writeErr(enc, err)
				continue
			}
			err := prov.CompleteStep(ctx, payload.RunID, payload.StepID, payload.Actor, payload.Note)
			write(enc, map[string]string{"status": "ok"}, err)
		default:
			writeErr(enc, fmt.Errorf("unknown method: %s", req.Method))
		}
	}
}

func ensureProvider(cfg map[string]any) (coreorchestration.Provider, error) {
	if provider != nil {
		return provider, nil
	}
	prov, err := adapter.New(cfg)
	if err != nil {
		return nil, err
	}
	provider = prov
	return provider, nil
}

func write(enc *json.Encoder, result any, err error) {
	if err != nil {
		writeErr(enc, err)
		return
	}
	_ = enc.Encode(rpcResponse{Result: result})
}

func writeErr(enc *json.Encoder, err error) {
	_ = enc.Encode(rpcResponse{Error: err.Error()})
}
