package tools

// ToolKeepListening asks the satellite to listen again after the answer,
// without a question at its end (docs/spec/satellit.md). The engine sees the
// call and sets continue_conversation; the tool itself is in
// internal/satellite.
const ToolKeepListening = "keep_listening"
