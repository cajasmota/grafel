// nest_app.ts — NestJS-flavoured module entry point.
// Proves import_resolution_quality for the NestJS family: SERVER_PORT is
// imported from ./config and re-used as the listen-port literal; the substrate
// cross-file resolution must reach through this import to the declaring file.
import { NestFactory } from "@nestjs/core";
import { AppModule } from "./app_module";
import { SERVER_PORT, API_BASE_URL } from "./config";

async function bootstrap() {
  const app = await NestFactory.create(AppModule);
  // SERVER_PORT and API_BASE_URL are resolved across the module boundary by
  // the constant-propagation pass following the IMPORTS edge from this file
  // back to config.ts.
  await app.listen(SERVER_PORT);
  console.log(`Application running on ${API_BASE_URL}`);
}

bootstrap();
