'use strict';

// One authored base shared by startup and the dynamic grade's release path.
// A milder contrast curve keeps dark alloy detail visible against deep space.
export const ARENA_GRADE = Object.freeze({ exposure: 1.12, contrast: 1.04, vignetteWeight: 1.15 });

export function applyArenaGrade(imageProcessing) {
  imageProcessing.exposure = ARENA_GRADE.exposure;
  imageProcessing.contrast = ARENA_GRADE.contrast;
  imageProcessing.vignetteWeight = ARENA_GRADE.vignetteWeight;
  if (imageProcessing.vignetteColor) {
    imageProcessing.vignetteColor.r = 0.008;
    imageProcessing.vignetteColor.g = 0.015;
    imageProcessing.vignetteColor.b = 0.035;
  }
}
