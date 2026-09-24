from pydantic.v1 import BaseModel


class User(BaseModel):
    name: str
