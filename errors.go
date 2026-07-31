package agentplan

import (
	"errors"
	"fmt"
)

const (
	ErrorCodeInvalidArgument   = "invalid_argument"
	ErrorCodePlanNotFound      = "plan_not_found"
	ErrorCodePlanUnreadable    = "plan_unreadable"
	ErrorCodeTaskNotFound      = "task_not_found"
	ErrorCodeTaskRecordMissing = "task_record_missing"
	ErrorCodePathOutsidePlan   = "path_outside_plan"
	ErrorCodeManifestInvalid   = "manifest_invalid"
	ErrorCodeCursorStale       = "cursor_stale"
)

func invalidArgumentError(err error) error {
	if err == nil {
		return nil
	}
	return &PlanError{
		ErrorCode: ErrorCodeInvalidArgument,
		Message:   err.Error(),
		Err:       err,
	}
}

type PlanError struct {
	ErrorCode string
	Message   string
	PlanID    string
	TaskID    string
	Path      string
	Err       error
}

func (e *PlanError) Error() string {
	if e == nil {
		return ""
	}
	message := e.Message
	if message == "" {
		message = e.ErrorCode
	}
	switch {
	case e.TaskID != "":
		return fmt.Sprintf("%s: %s", message, e.TaskID)
	case e.PlanID != "":
		return fmt.Sprintf("%s: %s", message, e.PlanID)
	default:
		return message
	}
}

func (e *PlanError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *PlanError) Code() string {
	if e == nil {
		return ""
	}
	return e.ErrorCode
}

type ErrorObject struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	PlanID  string `json:"plan_id,omitempty"`
	TaskID  string `json:"task_id,omitempty"`
	Path    string `json:"path,omitempty"`
}

type ErrorEnvelope struct {
	OK    bool        `json:"ok"`
	Error ErrorObject `json:"error"`
}

func errorObject(err error) ErrorObject {
	var planErr *PlanError
	if errors.As(err, &planErr) {
		return ErrorObject{
			Code:    planErr.ErrorCode,
			Message: planErr.Message,
			PlanID:  planErr.PlanID,
			TaskID:  planErr.TaskID,
			Path:    planErr.Path,
		}
	}
	return ErrorObject{Code: ErrorCodePlanUnreadable, Message: err.Error()}
}

func errorHasCode(err error, code string) bool {
	var planErr *PlanError
	return errors.As(err, &planErr) && planErr.ErrorCode == code
}
