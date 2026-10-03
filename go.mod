module github.com/hyz-is/arandu-liveness

go 1.26.0

require (
	github.com/arandu-io/framework v0.50.1
	github.com/hyz-is/arandu-face v0.0.0-00010101000000-000000000000
	github.com/hyz-is/arandu-onnx v0.0.0-00010101000000-000000000000
)

require (
	github.com/arandu-io/hesape v0.44.1 // indirect
	github.com/yalue/onnxruntime_go v1.31.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

// arandu-onnx and arandu-face are not published yet. The replaces resolve them
// from the sibling checkouts and are removed once those modules are tagged.
replace (
	github.com/hyz-is/arandu-face => ../arandu-face
	github.com/hyz-is/arandu-onnx => ../arandu-onnx
)
