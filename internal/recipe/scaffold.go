package recipe

// Scaffold is a safe recipe template with no shell transform.
func Scaffold() string {
	return `apiVersion: evolvectl.dev/v1alpha1
kind: MigrationRecipe
metadata:
  id: example.call-rename
  title: Rename an example call
  owners:
    - your-team
  tags:
    - example
spec:
  language: go
  dependency:
    ecosystem: gomod
    name: example.com/lib
    from: ">=1.0.0 <2.0.0"
    to: ">=2.0.0"
  match:
    type: call
    package: example.com/lib
    symbol: Old
  transform:
    type: call_rename
    target_symbol: New
  safety:
    confidence: high
    generated_files: deny
    require_type_info: true
  validate:
    - gofmt
`
}
