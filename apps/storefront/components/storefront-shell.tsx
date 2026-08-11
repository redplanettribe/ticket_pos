import { StorefrontShell as BaseStorefrontShell } from "@ticket-pos/ui";
import { getLocale, getTranslations } from "next-intl/server";
import type { ComponentProps } from "react";

import { LanguageSwitcher } from "@/components/language-switcher";
import { BRAND_NAME } from "@/lib/brand";
import { localizedPath, toAppLocale } from "@/lib/locale";
import { PRIVACY_POLICY_PATH } from "@/lib/privacy-policy";

type BaseProps = ComponentProps<typeof BaseStorefrontShell>;

/**
 * The shared Storefront shell, wearing the locale of the page inside it.
 *
 * The shell in @ticket-pos/ui knows nothing about locales and should not: it is
 * markup shared with Staff, which has none. The one address it holds on its own
 * is the mark's link home, and that is exactly the link that must not silently
 * change language — a Customer reading Spanish who taps the logo and lands in
 * English has been thrown out of the Storefront they were in, because "/"
 * resolves by cookie and browser rather than by where they already were.
 *
 * The chrome's own words arrive the same way. The shell's label props default
 * to English, which is right for a package that cannot know what language its
 * caller is in; this wrapper is the one place that does know, so it fills them
 * from the catalog's `shell` namespace and no page has to remember to. The home
 * link's label is the exception and stays a constant: it is the brand name, and
 * a brand name is not translated (lib/brand.ts).
 *
 * The language switcher is filled in here rather than page by page, and it is
 * not a prop a page may pass: every page must offer both languages, because the
 * link to a page's translation is how a crawler discovers that the translation
 * exists at all (components/language-switcher.tsx). A page that could opt out
 * would opt out silently.
 *
 * The locale comes from the request rather than from props, which is what keeps
 * the pages themselves unchanged apart from the import.
 */
export async function StorefrontShell(
  props: Omit<
    BaseProps,
    | "homeHref"
    | "homeLinkLabel"
    | "poweredByLabel"
    | "organizationLogoAlt"
    | "languageSwitcher"
    | "privacyHref"
    | "privacyLabel"
  >,
) {
  const locale = toAppLocale(await getLocale());
  const t = await getTranslations("shell");
  return (
    <BaseStorefrontShell
      {...props}
      homeHref={localizedPath(locale, "/")}
      homeLinkLabel={BRAND_NAME}
      languageSwitcher={<LanguageSwitcher />}
      poweredByLabel={t("poweredBy", { brand: BRAND_NAME })}
      // The Privacy Policy link, on every page's footer and not a prop a page
      // may pass — for the reason the language switcher is not one. A privacy
      // notice has to be reachable from wherever a person happens to be when
      // they wonder about it, and a page that could opt out would opt out
      // silently. Localized here, like the mark's link home: this is the one
      // place that knows which language is being read.
      privacyHref={localizedPath(locale, PRIVACY_POLICY_PATH)}
      privacyLabel={t("privacyPolicy")}
      // Only the header with an Organization in it draws a logo; without a name
      // there is nothing to interpolate and the package's own fallback is the
      // better answer.
      organizationLogoAlt={
        props.organizationName
          ? t("organizationLogoAlt", { organization: props.organizationName })
          : undefined
      }
    />
  );
}
