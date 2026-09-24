from pydantic import validator


class User(object):
    @validator("name")
    def check_name(cls, v):
        return v
