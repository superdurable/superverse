package process

import (
	"log/slog"
	"strings"

	"github.com/superdurable/dex/sdk-go/dex"
)

// maximumLoggedErrorBytes bounds the error text of one failure line.
const maximumLoggedErrorBytes = 2048

// ErrorTail is the end of an error's text that a failure line logs.
func ErrorTail(err error) string {
	text := strings.ToValidUTF8(err.Error(), "?")
	if len(text) > maximumLoggedErrorBytes {
		text = "..." + strings.ToValidUTF8(text[len(text)-maximumLoggedErrorBytes:], "")
	}
	return text
}

// logFailure logs a Step Execute or RPC handler that returned an error. The
// Dex Worker logs only panics, so without it such a failure leaves no line in
// the Preview or Live log. Every handler defers it with its named error
// result:
//
//	defer logFailure(ctx, TextSummaryFlow{}, "PrepareSummary", &err)
//
// A line names the Flow type, the FlowID, the handler, the attempt and the end
// of the error, never an input, an output or an Attribute value.
func logFailure(ctx dex.Context, flow dex.Flow, handler string, err *error) {
	if *err != nil {
		slog.ErrorContext(ctx, "Dex handler failed", "flowType", dex.GetFinalFlowType(flow), "flowId", ctx.FlowID(),
			"handler", handler, "attempt", ctx.Attempt(), "error", ErrorTail(*err))
	}
}
