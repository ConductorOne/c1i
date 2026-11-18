package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const (
	invocationPollInterval = 2 * time.Second
	invocationWaitTimeout  = 5 * time.Minute
	invocationSucceeded    = "FUNCTION_INVOCATION_STATUS_SUCCESS"
)

// invocationFailed are the statuses after which an invocation will not succeed.
var invocationFailed = map[string]bool{
	"FUNCTION_INVOCATION_STATUS_ERROR":     true,
	"FUNCTION_INVOCATION_STATUS_CANCELLED": true,
}

var functionsInvokeCmd = &cobra.Command{
	Use:   "invoke <function-id>",
	Short: "Run a function",
	Long: `Start an invocation of a function and print the API's response, whose
.invocationId names it. The run is asynchronous: read its status, output and
error with "functions invocations get <invocation-id> --function-id <id>".

Without --commit-id, c1i reads the function and runs its published commit; a
function with nothing published exits 2 (sent without a commit id, the API
answers it with a 400, "commit_id is required"). A --commit-id is checked first: one the function does not have exits 4,
since the server would otherwise accept it and leave the invocation running.
Both reads also run on --dry-run.

--input (or --input-file, "-" for stdin) is the handler's input and must be a
JSON object; omit both to send none (a handler then receives {}).

With --wait, c1i polls the invocation every 2s after printing the response and
exits 0 once it succeeds. Any --wait failure exits 1 with an error saying the
invoke "was applied", which means only that the invocation was started. A run
that failed is the error naming "ended in FUNCTION_INVOCATION_STATUS_ERROR"
(or CANCELLED) and the function's error; a timeout says it timed out and is
not necessarily a failure. The run's output is in "functions invocations get".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		functionID := args[0]
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}
		body := map[string]any{}
		input, err := invocationInput(cmd)
		if err != nil {
			return err
		}
		if input != nil {
			body["json"] = input // []byte: sent base64-encoded, as the API takes it
		}
		commitID, _ := cmd.Flags().GetString("commit-id")
		path := client.Path("/api/v1/functions/%s/invoke", functionID)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body},
			invokeCommitPreRead(functionID, commitID),
			withResponse(func(c *client.Client, resp []byte) writeOutcome {
				var r struct {
					InvocationID string `json:"invocationId"`
				}
				if json.Unmarshal(resp, &r) != nil || r.InvocationID == "" {
					if !wait {
						return writeOutcome{}
					}
					return writeOutcome{Err: &nonJSONResponseError{errors.New("the invoke response held no invocationId to wait on")}}
				}
				return writeOutcome{Wait: invocationWaitOp(c, functionID, r.InvocationID, waitTimeout)}
			}, wait))
	},
}

// invocationInput reads --input or --input-file and refuses anything but a
// JSON object, which the API also refuses. nil when neither was given.
func invocationInput(cmd *cobra.Command) ([]byte, error) {
	f := cmd.Flags()
	var data []byte
	label := "--input"
	switch {
	case flagGiven(cmd, "input") && flagGiven(cmd, "input-file"):
		return nil, &usageError{errors.New("--input and --input-file are mutually exclusive")}
	case flagGiven(cmd, "input"):
		s, _ := f.GetString("input")
		data = []byte(s)
	case flagGiven(cmd, "input-file"):
		label = "--input-file"
		p, _ := f.GetString("input-file")
		var err error
		if data, err = readJSONBytes(cmd, p); err != nil {
			return nil, &usageError{fmt.Errorf("reading %s: %w", label, err)}
		}
	default:
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return nil, &usageError{fmt.Errorf("%s must be a JSON object", label)}
	}
	return data, nil
}

// invokeCommitPreRead sets the request's commitId: the given one once the
// function is shown to have it, else the function's published commit.
func invokeCommitPreRead(functionID, commitID string) mutationOpt {
	return withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
		body := m.Body.(map[string]any)
		if commitID != "" {
			if _, err := c.Get(ctx, client.Path("/api/v1/functions/%s/commits/%s", functionID, commitID), nil); err != nil {
				return nil, fmt.Errorf("checking --commit-id: %w", err)
			}
			body["commitId"] = commitID
			return nil, nil
		}
		published, _, err := functionCommits(ctx, c, functionID)
		if err != nil {
			return nil, err
		}
		if published == "" {
			return nil, &usageError{fmt.Errorf("function %s has no published commit; pass --commit-id, or publish one with \"functions update %s --publish-commit <commit-id>\"", functionID, functionID)}
		}
		body["commitId"] = published
		return []resolvedID{{Name: "commitId", Label: "published commit", ID: published}}, nil
	}})
}

// invocationWaitOp polls one invocation until it succeeds. A failed terminal
// status is a Poll error, which ends the wait and the command non-zero.
func invocationWaitOp(c *client.Client, functionID, invocationID string, timeout time.Duration) waitOp[[]string] {
	path := client.Path("/api/v1/functions/%s/invocations/%s", functionID, invocationID)
	return waitOp[[]string]{
		Poll: func(ctx context.Context) ([]string, error) {
			data, err := c.Get(ctx, path, nil)
			if err != nil {
				return nil, fmt.Errorf("API error: %w", err)
			}
			var got struct {
				Invocation *struct {
					Status string `json:"status"`
					Error  string `json:"error"`
				} `json:"invocation"`
			}
			if err := json.Unmarshal(data, &got); err != nil || got.Invocation == nil || got.Invocation.Status == "" {
				return nil, &nonJSONResponseError{fmt.Errorf("invocation %s: the response held no invocation.status", invocationID)}
			}
			if invocationFailed[got.Invocation.Status] {
				return nil, fmt.Errorf("invocation %s ended in %s: %s", invocationID, got.Invocation.Status, got.Invocation.Error)
			}
			return []string{got.Invocation.Status}, nil
		},
		Done:     func(s []string) bool { return len(s) == 1 && s[0] == invocationSucceeded },
		Interval: invocationPollInterval,
		Timeout:  timeout,
		Subject:  "invocation " + invocationID,
		Success:  "Invocation " + invocationID + " succeeded",
		Slow:     "the invocation may still be queued or running",
		Recheck:  fmt.Sprintf("c1i functions invocations get %s --function-id %s", invocationID, functionID),
	}
}

func init() {
	f := functionsInvokeCmd.Flags()
	f.String("commit-id", "", "Commit to run (default: the function's published commit)")
	f.String("input", "", "Handler input as a JSON object (e.g. '{\"a\":1}')")
	f.String("input-file", "", "File holding the handler input JSON object (\"-\" for stdin)")
	addWaitFlags(functionsInvokeCmd, "GET .../invocations/<id> until the invocation succeeds (exit 0) or fails (exit 1)", invocationWaitTimeout)
	markMutating(functionsInvokeCmd)
	functionsCmd.AddCommand(functionsInvokeCmd)
}
