// scrollbar utility classes using css variables for theme support
// these match the scrollbar styling defined in app.css

export const scrollBarClassesHorizontal = `
    [&::-webkit-scrollbar]:h-1.5
    [&::-webkit-scrollbar-track]:bg-[var(--color-scrollbar-track)]
    [&::-webkit-scrollbar-track]:rounded-full
    [&::-webkit-scrollbar-thumb]:rounded-full
    [&::-webkit-scrollbar-thumb]:bg-[var(--color-scrollbar-thumb)]
    [&::-webkit-scrollbar-thumb:hover]:bg-[var(--color-scrollbar-thumb-hover)]
  `;

export const scrollBarClassesVertical = `
    [&::-webkit-scrollbar]:w-1.5
    [&::-webkit-scrollbar-track]:bg-[var(--color-scrollbar-track)]
    [&::-webkit-scrollbar-track]:rounded-full
    [&::-webkit-scrollbar-thumb]:rounded-full
    [&::-webkit-scrollbar-thumb]:bg-[var(--color-scrollbar-thumb)]
    [&::-webkit-scrollbar-thumb:hover]:bg-[var(--color-scrollbar-thumb-hover)]
  `;

// combined classes for containers that may scroll in both directions
export const scrollBarClasses = `${scrollBarClassesVertical} ${scrollBarClassesHorizontal}`;
