<script lang="ts">
  // Imports
  import { activeThing, openedApps } from "$store/store";
  import { createEventDispatcher } from "svelte";

  // Exports
  export let appName = "App Name";
  export let title = "App Name";
  export let darkBg = false;
  export let canGoBack = false;
  export let canReduce = true;
  export let canMaximize = true;
  export let canClose = true;
  export let customClose = false; // If true, only dispatch event, don't do default close behavior
  export let color = ""; // Custom background color

  // Constants
  const dispatch = createEventDispatcher();

  // Functions
  let onClickClose = () => {
    if (customClose) {
      // Only dispatch event, let parent handle everything
      dispatch("close");
    } else {
      // Default behavior
      $activeThing = "";
      $openedApps = $openedApps.filter((oa) => oa !== appName);
    }
  };
  let onClickMaximize = () => dispatch("maximize");
  let onClickReduce = () => dispatch("reduce");
  let onClickGoBack = () => dispatch("goBack");
</script>

<div
  class="title-bar"
  class:text-white={darkBg || color}
  style:background={color}
>
  <div class="actions">
    {#if canGoBack}
      <button class="hvrBgDark" on:click={onClickGoBack}>
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 16.933333 16.933334"
          height="13"
          width="13"
        >
          <g>
            <path
              id="path1159"
              style="color:#000000;stroke-width:1;stroke-linecap:round;-inkscape-stroke:none"
              d="M 15.743856,8.4669366 A 0.66145997,0.66145997 0 0 0 15.081881,7.8049613 H 2.908981 L 8.1562058,2.6373183 A 0.66145997,0.66145997 0 0 0 8.3530931,2.1701633 0.66145997,0.66145997 0 0 0 8.1639573,1.7014581 0.66145997,0.66145997 0 0 0 7.2280972,1.6937066 L 1.771066,7.067022 c -0.77545188,0.7637116 -0.77545188,2.0350837 0,2.7987955 l 5.4570312,5.3727985 a 0.66145997,0.66145997 0 0 0 0.9358601,-0.0057 0.66145997,0.66145997 0 0 0 -0.00775,-0.93586 L 2.9069141,9.1273612 H 15.081881 a 0.66145997,0.66145997 0 0 0 0.661975,-0.6604246 z"
            />
          </g>
        </svg>
      </button>
    {/if}
  </div>

  <div class="app-ident" class:pl-4={!canGoBack}>
    <slot>
      {#if !canGoBack}
        <img
          src="img/icon/{appName}.svg"
          alt="Icon of {appName}"
          height="16"
          width="16"
        />
      {/if}
      <span class="appName pl-2">{title || appName}</span>
    </slot>
  </div>

  <div class="actions">
    {#if canReduce}
      <button class="hvrBgDark" on:click={onClickReduce}>
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 16.933333 16.933334"
          height="13"
          width="13"
        >
          <g>
            <path
              style="color:#000000;stroke-linecap:round;-inkscape-stroke:none"
              d="M 1.8515625,7.8046875 A 0.66145998,0.66145998 0 0 0 1.1914062,8.4667969 0.66145998,0.66145998 0 0 0 1.8515625,9.1289063 H 15.082031 A 0.66145998,0.66145998 0 0 0 15.742188,8.4667969 0.66145998,0.66145998 0 0 0 15.082031,7.8046875 Z"
              id="path888"
            />
          </g>
        </svg>
      </button>
    {/if}

    {#if canMaximize}
      <button class="hvrBgDark" on:click={onClickMaximize}>
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 16.933333 16.933334"
          height="13"
          width="13"
        >
          <g>
            <path
              id="path841"
              d="M 3.1757812 1.1914062 C 2.087675 1.1914062 1.1914062 2.087675 1.1914062 3.1757812 L 1.1914062 13.757812 C 1.1914068 14.845918 2.0876754 15.742188 3.1757812 15.742188 L 13.757812 15.742188 C 14.845918 15.742187 15.742187 14.845918 15.742188 13.757812 L 15.742188 3.1757812 C 15.742188 2.0876754 14.845918 1.1914068 13.757812 1.1914062 L 3.1757812 1.1914062 z M 3.1757812 2.5136719 L 13.757812 2.5136719 C 14.13096 2.5136721 14.419922 2.8026342 14.419922 3.1757812 L 14.419922 13.757812 C 14.419922 14.130959 14.130959 14.419922 13.757812 14.419922 L 3.1757812 14.419922 C 2.8026342 14.419922 2.5136721 14.13096 2.5136719 13.757812 L 2.5136719 3.1757812 C 2.5136719 2.802634 2.802634 2.5136719 3.1757812 2.5136719 z "
              style="color:#000000;font-style:normal;font-variant:normal;font-weight:normal;font-stretch:normal;font-size:medium;line-height:normal;font-family:sans-serif;font-variant-ligatures:normal;font-variant-position:normal;font-variant-caps:normal;font-variant-numeric:normal;font-variant-alternates:normal;font-variant-east-asian:normal;font-feature-settings:normal;font-variation-settings:normal;text-indent:0;text-align:start;text-decoration:none;text-decoration-line:none;text-decoration-style:solid;text-decoration-color:#000000;letter-spacing:normal;word-spacing:normal;text-transform:none;writing-mode:lr-tb;direction:ltr;text-orientation:mixed;dominant-baseline:auto;baseline-shift:baseline;text-anchor:start;white-space:normal;shape-padding:0;shape-margin:0;inline-size:0;clip-rule:nonzero;display:inline;overflow:visible;visibility:visible;isolation:auto;mix-blend-mode:normal;color-interpolation:sRGB;color-interpolation-filters:linearRGB;solid-color:#000000;solid-opacity:1;vector-effect:none;fill-opacity:1;fill-rule:nonzero;stroke:none;stroke-width:1.32292;stroke-linecap:round;stroke-linejoin:miter;stroke-miterlimit:4;stroke-dasharray:none;stroke-dashoffset:0;stroke-opacity:1;color-rendering:auto;image-rendering:auto;shape-rendering:auto;text-rendering:auto;enable-background:accumulate;stop-color:#000000;stop-opacity:1;opacity:1"
            />
          </g>
        </svg>
      </button>
    {/if}

    {#if canClose}
      <button
        class="hover:bg-red-600 btn-close"
        on:click={onClickClose}
        on:keypress={onClickClose}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 16.933333 16.933334"
          height="13"
          width="13"
        >
          <g>
            <path
              id="path839"
              d="M 2.4863281 1.8535156 A 0.66145998 0.66145998 0 0 0 2.0449219 2.0449219 A 0.66145998 0.66145998 0 0 0 2.0449219 2.9804688 L 7.53125 8.4648438 L 2.0449219 13.951172 A 0.66145998 0.66145998 0 0 0 2.0449219 14.886719 A 0.66145998 0.66145998 0 0 0 2.9804688 14.886719 L 8.4648438 9.4023438 L 13.951172 14.886719 A 0.66145998 0.66145998 0 0 0 14.886719 14.886719 A 0.66145998 0.66145998 0 0 0 14.886719 13.951172 L 9.4023438 8.4648438 L 14.886719 2.9804688 A 0.66145998 0.66145998 0 0 0 14.886719 2.0449219 A 0.66145998 0.66145998 0 0 0 13.951172 2.0449219 L 8.4648438 7.53125 L 2.9804688 2.0449219 A 0.66145998 0.66145998 0 0 0 2.4863281 1.8535156 z "
              style="color:#000000;font-style:normal;font-variant:normal;font-weight:normal;font-stretch:normal;font-size:medium;line-height:normal;font-family:sans-serif;font-variant-ligatures:normal;font-variant-position:normal;font-variant-caps:normal;font-variant-numeric:normal;font-variant-alternates:normal;font-variant-east-asian:normal;font-feature-settings:normal;font-variation-settings:normal;text-indent:0;text-align:start;text-decoration:none;text-decoration-line:none;text-decoration-style:solid;text-decoration-color:#000000;letter-spacing:normal;word-spacing:normal;text-transform:none;writing-mode:lr-tb;direction:ltr;text-orientation:mixed;dominant-baseline:auto;baseline-shift:baseline;text-anchor:start;white-space:normal;shape-padding:0;shape-margin:0;inline-size:0;clip-rule:nonzero;display:inline;overflow:visible;visibility:visible;isolation:auto;mix-blend-mode:normal;color-interpolation:sRGB;color-interpolation-filters:linearRGB;solid-color:#000000;solid-opacity:1;vector-effect:none;fill-opacity:1;fill-rule:nonzero;stroke:none;stroke-width:1.32292;stroke-linecap:round;stroke-linejoin:miter;stroke-miterlimit:4;stroke-dasharray:none;stroke-dashoffset:0;stroke-opacity:1;color-rendering:auto;image-rendering:auto;shape-rendering:auto;text-rendering:auto;enable-background:accumulate;stop-color:#000000;stop-opacity:1;opacity:1"
            />
          </g>
        </svg>
      </button>
    {/if}
  </div>
</div>

<style lang="scss">
  .title-bar {
    display: flex;
    height: 36px;
    align-items: stretch;
    position: sticky;
    top: 0;
    z-index: 10;
    border-bottom: 1px solid var(--border-color);
  }

  .app-ident,
  .actions {
    display: flex;
    align-items: stretch;
    height: 100%;
  }

  .app-ident {
    align-items: center;
    flex: 1;
    color: rgb(var(--clr));
    font-weight: 500;
  }

  .actions button {
    display: flex;
    align-items: center;
    padding: 0 var(--sp-4);
    border: none;
    background: transparent;
    transition: var(--trans-fast);
    cursor: pointer;

    svg path {
      fill: rgb(var(--clr));
    }

    &:hover {
      background: rgb(var(--clr) / 8%);
    }

    &.btn-close:hover {
      background: #e81123 !important;
      svg path {
        fill: white !important;
      }
    }
  }

  @media (max-width: 768px) {
    .title-bar {
      height: 44px;
    }

    .actions button {
      padding: 0 var(--sp-4);
      min-width: 44px;

      svg {
        width: 16px;
        height: 16px;
      }
    }

    .app-ident {
      padding-left: var(--sp-3);
      font-size: var(--text-sm);
    }
  }
</style>
