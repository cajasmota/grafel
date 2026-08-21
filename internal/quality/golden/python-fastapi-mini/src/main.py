from fastapi import FastAPI

from app.routers.things import router

app = FastAPI(title="things-api")

app.include_router(router)
