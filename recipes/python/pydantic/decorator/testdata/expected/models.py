from pydantic import field_validator


class User(object):
    @field_validator("name")
    def check_name(cls, v):
        return v
