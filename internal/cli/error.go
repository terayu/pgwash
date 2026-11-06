package cli

// CodedError はヘッドレス実行の終了コード（設計案 11章）を運ぶエラー。
// cobra の RunE から返し、Execute() が終了コードへ変換する。
type CodedError struct {
	Code int
	Err  error
}

func (e *CodedError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *CodedError) Unwrap() error { return e.Err }

// NewCodedError は code を伴うエラーを生成する。err が nil なら nil を返す。
func NewCodedError(code int, err error) error {
	if err == nil {
		return nil
	}
	return &CodedError{Code: code, Err: err}
}
