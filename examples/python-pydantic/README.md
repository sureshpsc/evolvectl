# Python + Pydantic fixture

```bash
evolvectl plan python:pydantic --to 2.11.0 --workspace examples/python-pydantic
evolvectl upgrade python:pydantic --to 2.11.0 --workspace examples/python-pydantic
```

Recipes rename the import, `@validator`, `.dict()`, and the `orm_mode` keyword. A quoted `orm_mode=True` string is left unchanged. The required gate is a delimiter balance check, not a claim that pydantic itself was executed.
