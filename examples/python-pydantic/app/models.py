from pydantic.v1 import BaseModel


class User(BaseModel):
    @validator("name")
    def check_name(cls, v):
        return v


def export(model):
    return model.dict()


def build():
    return User(name="a", orm_mode=True)


note = "orm_mode=True"
