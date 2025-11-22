package utils

import "golang.org/x/crypto/bcrypt"

func HashPassword(p string)(string,error){
    b,err:=bcrypt.GenerateFromPassword([]byte(p),12)
    return string(b),err
}

func CheckPassword(h,p string)bool{
    return bcrypt.CompareHashAndPassword([]byte(h),[]byte(p))==nil
}
